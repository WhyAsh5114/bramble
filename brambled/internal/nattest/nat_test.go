//go:build nattest

package nattest_test

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/internal/nattest"
)

func server(t *testing.T, ip string) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(ip)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func client(t *testing.T, kind nattest.Type, ip string) (*nattest.NAT, net.PacketConn) {
	t.Helper()
	n, err := nattest.New(kind, net.ParseIP(ip))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	c, err := n.ListenPacket()
	if err != nil {
		t.Fatal(err)
	}
	return n, c
}

func send(t *testing.T, c net.PacketConn, to net.Addr, body string) {
	t.Helper()
	if n, err := c.WriteTo([]byte(body), to); err != nil || n != len(body) {
		t.Fatalf("send %q: n=%d err=%v", body, n, err)
	}
}

func receive(t *testing.T, c net.PacketConn, want string) net.Addr {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	defer c.SetReadDeadline(time.Time{})
	b := make([]byte, 2048)
	n, from, err := c.ReadFrom(b)
	if err != nil || string(b[:n]) != want {
		t.Fatalf("receive: got %q from %v err=%v, want %q", b[:n], from, err, want)
	}
	return from
}

func TestFullCone(t *testing.T) {
	nattest.RequireLoopbackRange(t)
	_, inside := client(t, nattest.FullCone, "127.0.0.2")
	stun := server(t, "127.0.0.10")
	peer := server(t, "127.0.0.11")
	send(t, inside, stun.LocalAddr(), "discover")
	public := receive(t, stun, "discover")
	send(t, stun, public, "binding response")
	if from := receive(t, inside, "binding response"); from.String() != stun.LocalAddr().String() {
		t.Fatalf("reply source = %v, want STUN address %v", from, stun.LocalAddr())
	}
	// The private socket has never contacted this peer.
	send(t, peer, public, "unsolicited")
	receive(t, inside, "unsolicited")
	send(t, inside, peer.LocalAddr(), "probe")
	if other := receive(t, peer, "probe"); other.String() != public.String() {
		t.Fatalf("cone mapping changed: %v -> %v", public, other)
	}
}

func expectTimeout(t *testing.T, c net.PacketConn) {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(60 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	defer c.SetReadDeadline(time.Time{})
	buf := make([]byte, 2048)
	if n, from, err := c.ReadFrom(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected dropped datagram: got %q from %v err=%v", buf[:n], from, err)
	}
}

func TestRestrictedCone(t *testing.T) {
	nattest.RequireLoopbackRange(t)
	_, inside := client(t, nattest.RestrictedCone, "127.0.0.2")
	stun := server(t, "127.0.0.10")
	peer := server(t, "127.0.0.11")
	peerOtherPort := server(t, "127.0.0.11")
	stranger := server(t, "127.0.0.12")
	send(t, inside, stun.LocalAddr(), "discover")
	public := receive(t, stun, "discover")
	send(t, peer, public, "unsolicited")
	expectTimeout(t, inside)
	send(t, inside, peer.LocalAddr(), "open")
	if mapped := receive(t, peer, "open"); mapped.String() != public.String() {
		t.Fatalf("restricted-cone mapping changed: %v -> %v", public, mapped)
	}
	// Restricted cone authorizes an IP, not just the contacted port.
	send(t, peerOtherPort, public, "other port")
	if from := receive(t, inside, "other port"); from.String() != peerOtherPort.LocalAddr().String() {
		t.Fatalf("source = %v, want %v", from, peerOtherPort.LocalAddr())
	}
	send(t, stranger, public, "still unsolicited")
	expectTimeout(t, inside)
	send(t, stun, public, "STUN remains authorized")
	receive(t, inside, "STUN remains authorized")
}

func TestSymmetric(t *testing.T) {
	nattest.RequireLoopbackRange(t)
	_, inside := client(t, nattest.Symmetric, "127.0.0.2")
	stun := server(t, "127.0.0.10")
	stunOtherPort := server(t, "127.0.0.10")
	peer := server(t, "127.0.0.11")
	send(t, inside, stun.LocalAddr(), "discover")
	stunMapping := receive(t, stun, "discover")
	send(t, stun, stunMapping, "binding response")
	receive(t, inside, "binding response")
	send(t, stunOtherPort, stunMapping, "wrong port")
	send(t, peer, stunMapping, "wrong IP")
	expectTimeout(t, inside)

	send(t, inside, stunOtherPort.LocalAddr(), "second destination")
	portMapping := receive(t, stunOtherPort, "second destination")
	send(t, inside, peer.LocalAddr(), "third destination")
	peerMapping := receive(t, peer, "third destination")
	if stunMapping.String() == portMapping.String() || stunMapping.String() == peerMapping.String() || portMapping.String() == peerMapping.String() {
		t.Fatalf("symmetric mappings must differ per destination: %v %v %v", stunMapping, portMapping, peerMapping)
	}
	send(t, peer, peerMapping, "peer reply")
	receive(t, inside, "peer reply")
	// Contacting a peer does not authorize it on the STUN mapping.
	send(t, peer, stunMapping, "wrong mapping")
	expectTimeout(t, inside)
	send(t, inside, stun.LocalAddr(), "repeat discovery")
	if repeated := receive(t, stun, "repeat discovery"); repeated.String() != stunMapping.String() {
		t.Fatalf("same destination did not reuse mapping: %v -> %v", stunMapping, repeated)
	}
}

// discover reflects the source address through the same socket used for probes.
// This tests the NAT seam, not the production STUN codec (ETH-36).
func discover(t *testing.T, inside net.PacketConn, reflector *net.UDPConn) net.Addr {
	t.Helper()
	send(t, inside, reflector.LocalAddr(), "where am I?")
	observed := receive(t, reflector, "where am I?")
	send(t, reflector, observed, observed.String())
	if err := inside.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	defer inside.SetReadDeadline(time.Time{})
	b := make([]byte, 128)
	n, _, err := inside.ReadFrom(b)
	if err != nil {
		t.Fatal(err)
	}
	public, err := net.ResolveUDPAddr("udp4", string(b[:n]))
	if err != nil {
		t.Fatal(err)
	}
	return public
}

func TestTwoNATs(t *testing.T) {
	nattest.RequireLoopbackRange(t)
	for _, kind := range []nattest.Type{nattest.FullCone, nattest.RestrictedCone, nattest.Symmetric} {
		t.Run(string(kind), func(t *testing.T) {
			_, alice := client(t, kind, "127.0.0.2")
			_, bob := client(t, kind, "127.0.0.3")
			stun := server(t, "127.0.0.10")
			alicePublic := discover(t, alice, stun)
			bobPublic := discover(t, bob, stun)
			// Retry after both sides have emitted a probe. No port prediction.
			for range 2 {
				send(t, alice, bobPublic, "alice probe")
				send(t, bob, alicePublic, "bob probe")
			}
			if kind == nattest.Symmetric {
				expectTimeout(t, alice)
				expectTimeout(t, bob)
				return
			}
			if from := receive(t, alice, "bob probe"); from.String() != bobPublic.String() {
				t.Fatalf("bob probe from %v, want %v", from, bobPublic)
			}
			if from := receive(t, bob, "alice probe"); from.String() != alicePublic.String() {
				t.Fatalf("alice probe from %v, want %v", from, alicePublic)
			}
		})
	}
}

func TestBlockDirectPathKeepsRelayReachable(t *testing.T) {
	nattest.RequireLoopbackRange(t)
	n, alice := client(t, nattest.RestrictedCone, "127.0.0.2")
	_, bob := client(t, nattest.RestrictedCone, "127.0.0.3")
	stun := server(t, "127.0.0.10")
	relay := server(t, "127.0.0.20")
	alicePublic := discover(t, alice, stun)
	bobPublic := discover(t, bob, stun)
	send(t, alice, bobPublic, "open alice")
	send(t, bob, alicePublic, "open bob")
	receive(t, alice, "open bob")
	// Consume the first probe if it arrived after Bob opened his filter.
	// A reply round trip proves the path is bidirectional without relying on
	// receive-goroutine scheduling of the first probe.
	send(t, alice, bobPublic, "direct")
	for {
		buf := make([]byte, 128)
		bob.SetReadDeadline(time.Now().Add(2 * time.Second))
		count, _, err := bob.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:count]) == "direct" {
			break
		}
	}

	n.SetBlocked(net.ParseIP("127.0.0.3"), true)
	send(t, alice, bobPublic, "blocked outbound")
	expectTimeout(t, bob)
	send(t, bob, alicePublic, "blocked inbound")
	expectTimeout(t, alice)
	// A dumb local relay forwards opaque bytes after both clients contact it.
	send(t, alice, relay.LocalAddr(), "register alice")
	aliceRelay := receive(t, relay, "register alice")
	send(t, bob, relay.LocalAddr(), "register bob")
	bobRelay := receive(t, relay, "register bob")
	send(t, alice, relay.LocalAddr(), "relay payload")
	receive(t, relay, "relay payload")
	send(t, relay, bobRelay, "relay payload")
	receive(t, bob, "relay payload")
	send(t, bob, relay.LocalAddr(), "relay reply")
	receive(t, relay, "relay reply")
	send(t, relay, aliceRelay, "relay reply")
	receive(t, alice, "relay reply")
	if repeated := discover(t, alice, stun); repeated.String() != alicePublic.String() {
		t.Fatalf("peer blocking changed STUN mapping: %v -> %v", alicePublic, repeated)
	}
	n.SetBlocked(net.ParseIP("127.0.0.3"), false)
	send(t, alice, bobPublic, "direct restored")
	receive(t, bob, "direct restored")
}

func TestPacketConnLifecycle(t *testing.T) {
	nattest.RequireLoopbackRange(t)
	n, inside := client(t, nattest.FullCone, "127.0.0.2")
	other, err := n.ListenPacket()
	if err != nil {
		t.Fatal(err)
	}
	peer := server(t, "127.0.0.10")
	send(t, inside, peer.LocalAddr(), "first socket")
	first := receive(t, peer, "first socket")
	send(t, other, peer.LocalAddr(), "second socket")
	second := receive(t, peer, "second socket")
	if first.String() == second.String() || inside.LocalAddr().String() == other.LocalAddr().String() {
		t.Fatal("private sockets shared a mapping or LAN address")
	}
	// Datagram truncation discards the tail; zero-length packets remain valid.
	send(t, peer, first, "abcdef")
	inside.SetReadDeadline(time.Now().Add(2 * time.Second))
	small := make([]byte, 3)
	if count, _, err := inside.ReadFrom(small); err != nil || count != 3 || string(small) != "abc" {
		t.Fatalf("truncation: %q count=%d err=%v", small, count, err)
	}
	send(t, peer, first, "")
	receive(t, inside, "")
	inside.SetWriteDeadline(time.Now().Add(-time.Second))
	if _, err := inside.WriteTo([]byte("late"), peer.LocalAddr()); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expired write deadline: %v", err)
	}
	inside.SetDeadline(time.Time{})
	send(t, inside, peer.LocalAddr(), "deadline cleared")
	receive(t, peer, "deadline cleared")

	readResult := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, _, err := inside.ReadFrom(make([]byte, 128))
		readResult <- err
	}()
	<-started
	inside.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	select {
	case err := <-readResult:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("pending read deadline: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("updating deadline did not unblock ReadFrom")
	}

	go func() {
		_, _, err := other.ReadFrom(make([]byte, 128))
		readResult <- err
	}()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readResult:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("read after NAT close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock ReadFrom")
	}
	if _, err := inside.WriteTo([]byte("closed"), peer.LocalAddr()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}
	if _, err := n.ListenPacket(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("listen after close: %v", err)
	}
	inside.Close()
	other.Close()
}

func TestConcurrentPackets(t *testing.T) {
	nattest.RequireLoopbackRange(t)
	_, inside := client(t, nattest.RestrictedCone, "127.0.0.2")
	const peers = 12
	completed := make(chan struct{}, peers)
	for range peers {
		peer := server(t, "127.0.0.10")
		go func() {
			defer func() { completed <- struct{}{} }()
			send(t, inside, peer.LocalAddr(), "concurrent")
			from := receive(t, peer, "concurrent")
			send(t, peer, from, "reply")
		}()
	}
	for range peers {
		receive(t, inside, "reply")
	}
	for range peers {
		<-completed
	}
}
