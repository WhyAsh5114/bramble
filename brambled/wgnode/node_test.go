package wgnode

import (
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/internal/testport"
)

// pingUntil retries Ping every second until it succeeds or deadline elapses.
func pingUntil(t *testing.T, from *Node, to netip.Addr, deadline time.Duration) bool {
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

// TestGate1_1_PeerRejectsUnregisteredKey is Gate 1.1's own test
// (docs/archive/hackathon/05_BUILD_PLAN.md): "Stand up a node, present a valid, well-formed
// WireGuard handshake from an unregistered key, assert refusal. Then
// register the key, assert acceptance after cache expiry."
//
// There is no separate "admission check" to call here — this test's whole
// point is that WireGuard's own protocol already does this: a device only
// completes a handshake with a public key present in its peer table.
func TestGate1_1_PeerRejectsUnregisteredKey(t *testing.T) {
	underTestPriv, underTestPub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating under-test key pair: %v", err)
	}
	peerPriv, peerPub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating peer key pair: %v", err)
	}

	underTestAddr := netip.MustParseAddr("10.99.0.1")
	peerAddr := netip.MustParseAddr("10.99.0.2")

	// Free ports, not fixed ones: fixed ports collide when several `go test`
	// processes share a host (see brambled/internal/testport).
	underTestPort := testport.FreeUDP(t)
	peerPort := testport.FreeUDP(t)

	underTest, err := New(Config{PrivateKeyHex: underTestPriv, ListenPort: underTestPort, LocalAddress: underTestAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting node under test: %v", err)
	}
	defer underTest.Close()

	peer, err := New(Config{PrivateKeyHex: peerPriv, ListenPort: peerPort, LocalAddress: peerAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting peer node: %v", err)
	}
	defer peer.Close()

	underTestEndpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), underTestPort)
	// The peer always knows who it's trying to reach; that's not what's under
	// test. What's under test is whether the node under test will complete a
	// handshake with a key it was never told about.
	if err := peer.AddPeer(underTestPub, netip.MustParsePrefix("10.99.0.1/32"), &underTestEndpoint); err != nil {
		t.Fatalf("configuring peer's view of the node under test: %v", err)
	}

	t.Run("unregistered key is refused", func(t *testing.T) {
		ok, err := peer.Ping(underTestAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("ping errored (want a clean timeout, not an error): %v", err)
		}
		if ok {
			t.Fatal("node under test completed a handshake with an unregistered key")
		}

		peers, err := underTest.Peers()
		if err != nil {
			t.Fatalf("reading node-under-test peer table: %v", err)
		}
		for _, p := range peers {
			if p.PublicKeyHex == peerPub && p.Handshaked() {
				t.Fatal("node under test recorded a handshake with an unregistered key")
			}
		}
	})

	t.Run("registering the key allows the handshake", func(t *testing.T) {
		peerEndpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), peerPort)
		if err := underTest.AddPeer(peerPub, netip.MustParsePrefix("10.99.0.2/32"), &peerEndpoint); err != nil {
			t.Fatalf("registering peer on node under test: %v", err)
		}

		// WireGuard enforces a RekeyTimeout (5s) between handshake initiation
		// attempts to the same peer — the previous sub-test's refused attempt
		// means "peer" won't send another initiation until that cools down.
		// Retrying past that window is the correct probe, not a single ping.
		if !pingUntil(t, peer, underTestAddr, 8*time.Second) {
			t.Fatal("node under test refused a handshake from a now-registered key")
		}

		peers, err := underTest.Peers()
		if err != nil {
			t.Fatalf("reading node-under-test peer table: %v", err)
		}
		for _, p := range peers {
			if p.PublicKeyHex == peerPub && p.Endpoint != peerEndpoint.String() {
				t.Fatalf("peer endpoint = %q, want %q", p.Endpoint, peerEndpoint)
			}
		}
	})
}
