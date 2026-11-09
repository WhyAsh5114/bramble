package stun

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
)

type result struct {
	endpoint netip.AddrPort
	err      error
}
type pending struct {
	server  string
	replies chan result
}

// Bind shares WireGuard's UDP sockets; only WireGuard's receive goroutines read
// them. STUN replies are consumed by those goroutines without blocking them.
type Bind struct {
	conn.Bind
	mu      sync.Mutex
	pending map[transactionID]pending
	closed  bool
	rto     time.Duration
}

func NewBind(inner conn.Bind) *Bind {
	return &Bind{Bind: inner, pending: make(map[transactionID]pending), closed: true, rto: 500 * time.Millisecond}
}

func (b *Bind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fns, actual, err := b.Bind.Open(port)
	if err != nil {
		return nil, 0, err
	}
	b.closed = false
	for i, receive := range fns {
		fns[i] = func(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
			n, err := receive(packets, sizes, eps)
			for j := 0; j < n; j++ {
				if sizes[j] > 0 && b.handlePunch(packets[j][:sizes[j]], eps[j]) {
					sizes[j] = 0
				}
				if sizes[j] > 0 && isSTUN(packets[j][:sizes[j]]) {
					b.handle(packets[j][:sizes[j]], eps[j])
					sizes[j] = 0 // ReceiveFunc explicitly permits empty slots.
				}
			}
			return n, err
		}
	}
	return fns, actual, nil
}

func (b *Bind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for id, p := range b.pending {
		select {
		case p.replies <- result{err: net.ErrClosed}:
		default:
		}
		delete(b.pending, id)
	}
	return b.Bind.Close()
}

func (b *Bind) handle(packet []byte, source conn.Endpoint) {
	var id transactionID
	copy(id[:], packet[8:20])
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pending[id]
	if !ok || source == nil || p.server != source.DstToString() {
		return
	}
	endpoint, err := response(packet)
	// Malformed packets cannot terminate an outstanding transaction.
	var rejection bindingError
	if err != nil && !errors.As(err, &rejection) {
		return
	}
	select {
	case p.replies <- result{endpoint, err}:
	default:
	}
}

// Binding sends one transaction, with RFC 5389's default seven transmissions,
// exponential backoff and final 16*RTO wait. Cancellation bounds all waits.
func (b *Bind) Binding(ctx context.Context, server netip.AddrPort) (netip.AddrPort, error) {
	if err := ctx.Err(); err != nil {
		return netip.AddrPort{}, err
	}
	ep, err := b.ParseEndpoint(server.String())
	if err != nil {
		return netip.AddrPort{}, err
	}
	var id transactionID
	if _, err := rand.Read(id[:]); err != nil {
		return netip.AddrPort{}, err
	}
	p := pending{server: ep.DstToString(), replies: make(chan result, 1)}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return netip.AddrPort{}, net.ErrClosed
	}
	b.pending[id] = p
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.pending, id); b.mu.Unlock() }()
	packet := request(id)
	for attempt := 0; attempt < 7; attempt++ {
		if err := ctx.Err(); err != nil {
			return netip.AddrPort{}, err
		}
		if err := b.Send([][]byte{packet}, ep); err != nil {
			return netip.AddrPort{}, err
		}
		delay := b.rto * time.Duration(1<<attempt)
		if attempt == 6 {
			delay = 16 * b.rto
		}
		timer := time.NewTimer(delay)
		select {
		case r := <-p.replies:
			timer.Stop()
			return r.endpoint, r.err
		case <-ctx.Done():
			timer.Stop()
			return netip.AddrPort{}, ctx.Err()
		case <-timer.C:
		}
	}
	return netip.AddrPort{}, fmt.Errorf("STUN binding timed out")
}

// Discover tries the configured host:port servers in order. DNS and each
// transaction are bounded; no public service is contacted unless configured.
func (b *Bind) Discover(ctx context.Context, servers []string) (netip.AddrPort, string, error) {
	var failures []error
	for _, server := range servers {
		if ctx.Err() != nil {
			return netip.AddrPort{}, "", ctx.Err()
		}
		host, portText, err := net.SplitHostPort(server)
		port, portErr := strconv.ParseUint(portText, 10, 16)
		if err != nil || portErr != nil || port == 0 || host == "" {
			failures = append(failures, fmt.Errorf("invalid STUN server host:port"))
			continue
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		ips, err := net.DefaultResolver.LookupNetIP(lookupCtx, "ip", host)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("STUN DNS lookup failed"))
			continue
		}
		for _, ip := range ips {
			queryCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
			endpoint, err := b.Binding(queryCtx, netip.AddrPortFrom(ip.Unmap(), uint16(port)))
			cancel()
			if err == nil {
				return endpoint, server, nil
			}
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 {
		return netip.AddrPort{}, "", fmt.Errorf("no STUN servers configured")
	}
	return netip.AddrPort{}, "", errors.Join(failures...)
}

// Send forwards to the inner bind with a fresh destination-only endpoint, so
// the kernel selects the current source/interface on every write. wireguard-go
// only installs its Linux route-change invalidation listener for a concrete
// *conn.StdNetBind, so a wrapper must not reuse the sticky source cache. The
// caller's endpoint is shared between goroutines and is never mutated.
func (b *Bind) Send(bufs [][]byte, ep conn.Endpoint) error {
	fresh, err := b.ParseEndpoint(ep.DstToString())
	if err != nil {
		return err
	}
	return b.Bind.Send(bufs, fresh)
}

// SendTo writes one raw datagram to target from the WireGuard socket, so a
// relay sees the same source address the WireGuard traffic will come from.
func (b *Bind) SendTo(target netip.AddrPort, payload []byte) error {
	ep, err := b.ParseEndpoint(target.String())
	if err != nil {
		return err
	}
	return b.Send([][]byte{payload}, ep)
}
