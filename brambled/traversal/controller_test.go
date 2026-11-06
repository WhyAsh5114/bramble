//go:build nattest

package traversal_test

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/internal/nattest"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/traversal"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
	"golang.zx2c4.com/wireguard/conn"
)

// packetBind adapts the NAT fixture at WireGuard's actual UDP socket boundary.
type packetBind struct {
	conn.Bind
	nat    *nattest.NAT
	socket net.PacketConn
}

func (b *packetBind) Open(uint16) ([]conn.ReceiveFunc, uint16, error) {
	var err error
	b.socket, err = b.nat.ListenPacket()
	if err != nil {
		return nil, 0, err
	}
	return []conn.ReceiveFunc{func(bufs [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		n, addr, err := b.socket.ReadFrom(bufs[0])
		if err != nil {
			return 0, err
		}
		sizes[0] = n
		eps[0], err = b.ParseEndpoint(addr.String())
		return 1, err
	}}, uint16(b.socket.LocalAddr().(*net.UDPAddr).Port), nil
}
func (b *packetBind) Close() error {
	if b.socket != nil {
		return b.socket.Close()
	}
	return nil
}
func (b *packetBind) SetMark(uint32) error { return nil }
func (b *packetBind) BatchSize() int       { return 1 }
func (b *packetBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	addr, err := net.ResolveUDPAddr("udp4", ep.DstToString())
	if err != nil {
		return err
	}
	for _, buf := range bufs {
		if _, err = b.socket.WriteTo(buf, addr); err != nil {
			return err
		}
	}
	return nil
}

func stunServer(t *testing.T) string {
	t.Helper()
	s, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.10")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, src, err := s.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			if n != 20 {
				continue
			}
			reply := make([]byte, 32)
			copy(reply, buf[:20])
			binary.BigEndian.PutUint16(reply, 0x101)
			binary.BigEndian.PutUint16(reply[2:], 12)
			binary.BigEndian.PutUint16(reply[20:], 0x20)
			binary.BigEndian.PutUint16(reply[22:], 8)
			reply[25] = 1
			binary.BigEndian.PutUint16(reply[26:], src.Port()^0x2112)
			for i, v := range src.Addr().AsSlice() {
				reply[28+i] = v ^ buf[4+i]
			}
			s.WriteToUDPAddrPort(reply, src)
		}
	}()
	return s.LocalAddr().String()
}

type testPeer struct {
	node     *wgnode.Node
	pub      string
	nat      *nattest.NAT
	endpoint netip.AddrPort
	ip       netip.Addr
}

func newPeer(t *testing.T, kind nattest.Type, publicIP, tunnelIP, stun string) testPeer {
	t.Helper()
	router, err := nattest.New(kind, net.ParseIP(publicIP))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { router.Close() })
	priv, pub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	ip := netip.MustParseAddr(tunnelIP)
	node, err := wgnode.New(wgnode.Config{PrivateKeyHex: priv, Netstack: true, LocalAddress: ip, EnableSTUN: true, Bind: &packetBind{Bind: conn.NewDefaultBind(), nat: router}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(node.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ep, err := node.DiscoverEndpoint(ctx, []string{stun})
	if err != nil {
		t.Fatal(err)
	}
	return testPeer{node, pub, router, ep, ip}
}
func admit(t *testing.T, a, b testPeer) {
	t.Helper()
	if err := a.node.AddPeer(b.pub, netip.PrefixFrom(b.ip, 32), nil); err != nil {
		t.Fatal(err)
	}
}
func waitState(t *testing.T, events <-chan string, want string) {
	t.Helper()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case state := <-events:
			if state == want {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}
func start(t *testing.T, a, b testPeer, relay func(context.Context) (netip.AddrPort, error)) <-chan string {
	t.Helper()
	events := make(chan string, 100)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	c := traversal.Controller{Node: a.node, Peer: b.pub, Learn: func(context.Context) (rendezvous.Announcement, error) {
		return rendezvous.Announcement{{Kind: rendezvous.KindSTUN, Addr: b.endpoint}}, nil
	}, Relay: relay, HandshakeTimeout: 7 * time.Second, RetryInterval: time.Second, StallTimeout: 3 * time.Second, OnState: func(s string) { events <- s }}
	go func() { defer close(done); c.Run(ctx) }()
	return events
}
func TestConeNATDirectWireGuard(t *testing.T) {
	stun := stunServer(t)
	a := newPeer(t, nattest.RestrictedCone, "127.0.0.2", "10.77.0.1", stun)
	b := newPeer(t, nattest.RestrictedCone, "127.0.0.3", "10.77.0.2", stun)
	admit(t, a, b)
	admit(t, b, a)
	ea := start(t, a, b, nil)
	eb := start(t, b, a, nil)
	waitState(t, ea, "direct")
	waitState(t, eb, "direct")
	if ok, err := a.node.Ping(b.ip, time.Second); err != nil || !ok {
		t.Fatalf("direct tunnel ping: %v %v", ok, err)
	}
}

// relayServer forwards opaque UDP between the first two source addresses, like
// the production port-per-session relay. It does not terminate WireGuard.
func relayServer(t *testing.T) netip.AddrPort {
	t.Helper()
	s, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.20")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	go func() {
		var peers []netip.AddrPort
		buf := make([]byte, 65535)
		for {
			n, src, err := s.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			known := false
			for _, peer := range peers {
				if peer == src {
					known = true
				}
			}
			if !known && len(peers) < 2 {
				peers = append(peers, src)
				known = true
			}
			if known && len(peers) == 2 {
				for _, peer := range peers {
					if peer != src {
						_, _ = s.WriteToUDPAddrPort(buf[:n], peer)
					}
				}
			}
		}
	}()
	return s.LocalAddr().(*net.UDPAddr).AddrPort()
}

func TestSymmetricNATFallsBackToRelay(t *testing.T) {
	stun := stunServer(t)
	a := newPeer(t, nattest.Symmetric, "127.0.0.2", "10.77.0.1", stun)
	b := newPeer(t, nattest.Symmetric, "127.0.0.3", "10.77.0.2", stun)
	admit(t, a, b)
	admit(t, b, a)
	relay := relayServer(t)
	fallback := func(context.Context) (netip.AddrPort, error) { return relay, nil }
	ea := start(t, a, b, fallback)
	eb := start(t, b, a, fallback)
	waitState(t, ea, "relay")
	waitState(t, eb, "relay")
	pingUntil(t, a, b)
}

func pingUntil(t *testing.T, a, b testPeer) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if ok, err := a.node.Ping(b.ip, 300*time.Millisecond); err == nil && ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("WireGuard tunnel ping failed")
}

func TestBlockedDirectFallsBackAndRecovers(t *testing.T) {
	stun := stunServer(t)
	a := newPeer(t, nattest.RestrictedCone, "127.0.0.2", "10.77.0.1", stun)
	b := newPeer(t, nattest.RestrictedCone, "127.0.0.3", "10.77.0.2", stun)
	admit(t, a, b)
	admit(t, b, a)
	relay := relayServer(t)
	fallback := func(context.Context) (netip.AddrPort, error) { return relay, nil }
	ea := start(t, a, b, fallback)
	eb := start(t, b, a, fallback)
	waitState(t, ea, "direct")
	waitState(t, eb, "direct")
	a.nat.SetBlocked(net.ParseIP("127.0.0.3"), true)
	waitState(t, ea, "relay")
	waitState(t, eb, "relay")
	pingUntil(t, a, b)
	a.nat.SetBlocked(net.ParseIP("127.0.0.3"), false)
	waitState(t, ea, "direct")
	waitState(t, eb, "direct")
	pingUntil(t, a, b)
}

func TestRevokedPeerCannotBeRecreatedByLatePathResult(t *testing.T) {
	stun := stunServer(t)
	a := newPeer(t, nattest.RestrictedCone, "127.0.0.2", "10.77.0.1", stun)
	b := newPeer(t, nattest.RestrictedCone, "127.0.0.3", "10.77.0.2", stun)
	admit(t, a, b)
	if err := a.node.RemovePeer(b.pub); err != nil {
		t.Fatal(err)
	}
	if err := a.node.SetPeerPath(b.pub, b.endpoint); err == nil {
		t.Fatal("revoked peer accepted path update")
	}
	if err := a.node.Punch(context.Background(), b.pub, b.endpoint); err == nil {
		t.Fatal("revoked peer sent probe")
	}
	peers, err := a.node.Peers()
	if err != nil || len(peers) != 0 {
		t.Fatalf("revoked peer returned: %v %v", peers, err)
	}
}

func TestProbeReplyDoesNotCountAsAuthenticatedDirectPath(t *testing.T) {
	stun := stunServer(t)
	a := newPeer(t, nattest.RestrictedCone, "127.0.0.2", "10.77.0.1", stun)
	b := newPeer(t, nattest.FullCone, "127.0.0.3", "10.77.0.2", stun)
	admit(t, a, b) // b can reply to probes, but does not admit a's WireGuard key.
	relay := relayServer(t)
	events := start(t, a, b, func(context.Context) (netip.AddrPort, error) { return relay, nil })
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	for {
		select {
		case state := <-events:
			if state == "direct" {
				t.Fatal("unauthenticated probe marked direct")
			}
			if state == "relay" {
				return
			}
		case <-timer.C:
			t.Fatal("did not fall back after unauthenticated probe replies")
		}
	}
}
