//go:build nattest

// Package nattest provides loopback-only UDP NAT fixtures for integration tests.
// Build with -tags nattest; production builds cannot import this package.
package nattest

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// Type names the mapping and filtering behavior of a simulated NAT.
type Type string

const (
	FullCone       Type = "full-cone"
	RestrictedCone Type = "restricted-cone"
	Symmetric      Type = "symmetric"
)

// NAT owns private packet connections and their public loopback UDP mappings.
// Close it after the test to close every socket and join its receive goroutines.
type NAT struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	kind     Type
	ip       net.IP
	closed   bool
	nextPort int
	clients  map[*packetConn]struct{}
	blocked  map[string]bool
}

// New creates a NAT. publicIP must be a loopback IPv4 address. Use different
// addresses for different NATs and servers so IP filtering remains meaningful.
func New(kind Type, publicIP net.IP) (*NAT, error) {
	if kind != FullCone && kind != RestrictedCone && kind != Symmetric {
		return nil, fmt.Errorf("unknown NAT type %q", kind)
	}
	if publicIP.To4() == nil || !publicIP.IsLoopback() {
		return nil, fmt.Errorf("NAT public IP must be loopback IPv4")
	}
	return &NAT{kind: kind, ip: append(net.IP(nil), publicIP.To4()...),
		clients: make(map[*packetConn]struct{}), blocked: make(map[string]bool)}, nil
}

// ListenPacket creates an independent private socket with a synthetic LAN
// address. WriteTo accepts only loopback IPv4 UDP destinations. Mapping sockets
// are allocated lazily on the first outbound packet and live until Close.
func (n *NAT) ListenPacket() (net.PacketConn, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, net.ErrClosed
	}
	n.nextPort++
	c := &packetConn{nat: n, local: net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: n.nextPort},
		mappings: make(map[string]*mapping), packets: make(chan packet, 256),
		done: make(chan struct{}), changed: make(chan struct{})}
	n.clients[c] = struct{}{}
	return c, nil
}

// SetBlocked silently drops both inbound and outbound traffic to a peer IP.
// Other IPs remain reachable, so tests can block a direct path but keep a relay.
func (n *NAT) SetBlocked(ip net.IP, blocked bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if blocked {
		n.blocked[ip.String()] = true
	} else {
		delete(n.blocked, ip.String())
	}
}

// Close closes private sockets and waits for all public receivers to exit.
// Repeated calls are safe.
func (n *NAT) Close() error {
	n.mu.Lock()
	n.closed = true
	for c := range n.clients {
		c.closeLocked()
	}
	n.mu.Unlock()
	n.wg.Wait()
	return nil
}

type mapping struct {
	socket  *net.UDPConn
	allowed map[string]bool
}

type packet struct {
	data []byte
	from *net.UDPAddr
}

type packetConn struct {
	nat           *NAT
	local         net.UDPAddr
	mappings      map[string]*mapping
	packets       chan packet
	done          chan struct{}
	closed        bool
	readDeadline  time.Time
	writeDeadline time.Time
	changed       chan struct{}
}

func (c *packetConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	to, ok := addr.(*net.UDPAddr)
	if !ok || to == nil || to.IP.To4() == nil || !to.IP.IsLoopback() || to.Port < 1 || to.Port > 65535 {
		return 0, fmt.Errorf("NAT destination must be loopback IPv4 UDP with a nonzero port")
	}
	c.nat.mu.Lock()
	defer c.nat.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	if !c.writeDeadline.IsZero() && !time.Now().Before(c.writeDeadline) {
		return 0, os.ErrDeadlineExceeded
	}
	if c.nat.blocked[to.IP.String()] {
		return len(b), nil
	}
	key := ""
	if c.nat.kind == Symmetric {
		key = to.String()
	}
	m := c.mappings[key]
	if m == nil {
		socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: c.nat.ip})
		if err != nil {
			return 0, err
		}
		m = &mapping{socket: socket, allowed: make(map[string]bool)}
		c.mappings[key] = m
		c.nat.wg.Add(1)
		go c.receive(m)
	}
	if err := m.socket.SetWriteDeadline(c.writeDeadline); err != nil {
		return 0, err
	}
	// Authorize before the send; a loopback reply can arrive immediately.
	m.allowed[c.filterKey(to)] = true
	return m.socket.WriteToUDP(b, to)
}

func (c *packetConn) filterKey(addr *net.UDPAddr) string {
	if c.nat.kind == Symmetric {
		return addr.String()
	}
	return addr.IP.String()
}

func (c *packetConn) receive(m *mapping) {
	defer c.nat.wg.Done()
	buf := make([]byte, 65535)
	for {
		count, from, err := m.socket.ReadFromUDP(buf)
		if err != nil {
			return
		}
		c.nat.mu.Lock()
		allowed := !c.closed && !c.nat.blocked[from.IP.String()] &&
			(c.nat.kind == FullCone || m.allowed[c.filterKey(from)])
		if allowed {
			// UDP can drop on overflow. Never hold shutdown hostage to a reader.
			select {
			case c.packets <- packet{data: append([]byte(nil), buf[:count]...), from: from}:
			default:
			}
		}
		c.nat.mu.Unlock()
	}
}

func (c *packetConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		c.nat.mu.Lock()
		deadline, changed, closed := c.readDeadline, c.changed, c.closed
		c.nat.mu.Unlock()
		if closed {
			return 0, nil, net.ErrClosed
		}
		var timer *time.Timer
		var timeout <-chan time.Time
		if !deadline.IsZero() {
			if !time.Now().Before(deadline) {
				return 0, nil, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(time.Until(deadline))
			timeout = timer.C
		}
		select {
		case p := <-c.packets:
			if timer != nil {
				timer.Stop()
			}
			return copy(b, p.data), p.from, nil
		case <-c.done:
			if timer != nil {
				timer.Stop()
			}
			return 0, nil, net.ErrClosed
		case <-changed:
			if timer != nil {
				timer.Stop()
			}
			// A new deadline also applies to a ReadFrom already in progress.
		case <-timeout:
			// Recheck the deadline in case a concurrent setter extended it.
		}
	}
}

func (c *packetConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: append(net.IP(nil), c.local.IP...), Port: c.local.Port}
}

func (c *packetConn) Close() error {
	c.nat.mu.Lock()
	defer c.nat.mu.Unlock()
	c.closeLocked()
	return nil
}

func (c *packetConn) closeLocked() {
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	for _, m := range c.mappings {
		m.socket.Close()
	}
	delete(c.nat.clients, c)
}

func (c *packetConn) SetDeadline(t time.Time) error {
	return c.setDeadline(t, true, true)
}

func (c *packetConn) SetReadDeadline(t time.Time) error {
	return c.setDeadline(t, true, false)
}

func (c *packetConn) SetWriteDeadline(t time.Time) error {
	return c.setDeadline(t, false, true)
}

func (c *packetConn) setDeadline(t time.Time, read, write bool) error {
	c.nat.mu.Lock()
	defer c.nat.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	if read {
		c.readDeadline = t
		close(c.changed)
		c.changed = make(chan struct{})
	}
	if write {
		c.writeDeadline = t
	}
	return nil
}
