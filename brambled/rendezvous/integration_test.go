package rendezvous

import (
	"io"
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// pingUntilConnected retries Ping every second until it succeeds or deadline
// elapses — WireGuard's own RekeyTimeout (5s between handshake initiation
// attempts to the same peer) means a single ping shortly after AddPeer can
// legitimately need more than one attempt.
func pingUntilConnected(t *testing.T, from *wgnode.Node, to netip.Addr, deadline time.Duration) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		ok, err := from.Ping(to, 1*time.Second)
		if err != nil {
			t.Fatalf("ping errored: %v", err)
		}
		if ok {
			return true
		}
	}
	return false
}

// TestRelayMediatedHandshake is Section C's CI-safe proof: two wgnode.Nodes
// that start out not knowing each other's address learn it purely by
// exchanging opaque candidates through a dumb relay (never consulted for
// authorization), then complete a real WireGuard handshake and carry both
// ICMP and TCP traffic. It runs on netstack (loopback, root-free) — it does
// not prove real NAT traversal or exercise the real OS TUN path; that's
// covered by the Part 1 local rehearsal and the actual Gate 0.2 run
// described in brambled/README.md's runbook.
func TestRelayMediatedHandshake(t *testing.T) {
	relayAddr := startFakeRelay(t)

	alicePriv, alicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating alice's key pair: %v", err)
	}
	bobPriv, bobPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating bob's key pair: %v", err)
	}

	aliceAddr := netip.MustParseAddr("10.98.0.1")
	bobAddr := netip.MustParseAddr("10.98.0.2")
	const aliceCandidate = "127.0.0.1:61920"
	const bobCandidate = "127.0.0.1:61921"

	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: 61920, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting alice: %v", err)
	}
	defer alice.Close()

	bob, err := wgnode.New(wgnode.Config{PrivateKeyHex: bobPriv, ListenPort: 61921, LocalAddress: bobAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting bob: %v", err)
	}
	defer bob.Close()

	// Neither side is told the other's address up front — only its public
	// key (as if resolved from ENS). The relay is what carries the address.
	type exchangeResult struct {
		candidate string
		err       error
	}
	aliceResult := make(chan exchangeResult, 1)
	bobResult := make(chan exchangeResult, 1)
	go func() {
		c, err := Exchange(relayAddr, alicePub, bobPub, aliceCandidate, "", 5*time.Second)
		aliceResult <- exchangeResult{c, err}
	}()
	go func() {
		c, err := Exchange(relayAddr, bobPub, alicePub, bobCandidate, "", 5*time.Second)
		bobResult <- exchangeResult{c, err}
	}()

	aliceLearned := <-aliceResult
	if aliceLearned.err != nil {
		t.Fatalf("alice's candidate exchange failed: %v", aliceLearned.err)
	}
	if aliceLearned.candidate != bobCandidate {
		t.Fatalf("alice learned the wrong candidate for bob: %q", aliceLearned.candidate)
	}

	bobLearned := <-bobResult
	if bobLearned.err != nil {
		t.Fatalf("bob's candidate exchange failed: %v", bobLearned.err)
	}
	if bobLearned.candidate != aliceCandidate {
		t.Fatalf("bob learned the wrong candidate for alice: %q", bobLearned.candidate)
	}

	// aliceLearned.candidate is what alice learned about bob (bobCandidate);
	// bobLearned.candidate is what bob learned about alice (aliceCandidate).
	bobsRealEndpoint, err := netip.ParseAddrPort(aliceLearned.candidate)
	if err != nil {
		t.Fatalf("parsing alice's view of bob's candidate: %v", err)
	}
	if err := alice.AddPeer(bobPub, netip.PrefixFrom(bobAddr, 32), &bobsRealEndpoint); err != nil {
		t.Fatalf("alice registering bob (relay-discovered endpoint): %v", err)
	}

	alicesRealEndpoint, err := netip.ParseAddrPort(bobLearned.candidate)
	if err != nil {
		t.Fatalf("parsing bob's view of alice's candidate: %v", err)
	}
	if err := bob.AddPeer(alicePub, netip.PrefixFrom(aliceAddr, 32), &alicesRealEndpoint); err != nil {
		t.Fatalf("bob registering alice (relay-discovered endpoint): %v", err)
	}

	if !pingUntilConnected(t, alice, bobAddr, 8*time.Second) {
		t.Fatal("no ping reply across the relay-discovered tunnel")
	}

	const payload = "bramble-gate0.2-tcp-check"
	ln, err := bob.ListenTCP(7000)
	if err != nil {
		t.Fatalf("bob listening for TCP: %v", err)
	}
	defer ln.Close()

	accepted := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			accepted <- ""
			return
		}
		defer conn.Close()
		buf, _ := io.ReadAll(conn)
		accepted <- string(buf)
	}()

	conn, err := alice.DialTCP(netip.AddrPortFrom(bobAddr, 7000))
	if err != nil {
		t.Fatalf("alice dialing bob over TCP: %v", err)
	}
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("writing TCP payload: %v", err)
	}
	conn.Close()

	select {
	case got := <-accepted:
		if got != payload {
			t.Fatalf("TCP round trip mismatch: got %q, want %q", got, payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for bob to receive the TCP payload")
	}
}
