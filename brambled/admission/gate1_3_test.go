package admission

import (
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

func pingUntil(t *testing.T, from *wgnode.Node, to netip.Addr, deadline time.Duration) bool {
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

// TestGate1_3_RevocationDropsAlreadyConnectedPeer is Gate 1.3's own concern
// (docs/05_BUILD_PLAN.md): the caution there warns that if an admission
// verifier only gates *new* handshakes, an already-connected revoked peer can
// keep passing traffic for up to WireGuard's own ~2 minute rekey timer,
// regardless of revocation. This test proves that's not what happens here:
// Loop.SyncOnce already calls PeerTable.RemovePeer the moment a tracked
// peer's record reads as unauthorized, and RemovePeer's IpcSet(remove=true)
// tears down the session key immediately — bounded by however often SyncOnce
// runs (the TTL), never by the rekey timer.
//
// Unlike loop_test.go's fakeTable, this wires Loop.Table to a real
// *wgnode.Node (netstack, root-free) so the assertion is about actual
// WireGuard behavior, not an in-memory map recording a call.
func TestGate1_3_RevocationDropsAlreadyConnectedPeer(t *testing.T) {
	underTestPriv, underTestPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating under-test key pair: %v", err)
	}
	peerPriv, peerPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating peer key pair: %v", err)
	}

	underTestAddr := netip.MustParseAddr("10.96.0.1")
	peerAddr := netip.MustParseAddr("10.96.0.2")

	underTest, err := wgnode.New(wgnode.Config{PrivateKeyHex: underTestPriv, ListenPort: 61940, LocalAddress: underTestAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting node under test: %v", err)
	}
	defer underTest.Close()

	peer, err := wgnode.New(wgnode.Config{PrivateKeyHex: peerPriv, ListenPort: 61941, LocalAddress: peerAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting peer node: %v", err)
	}
	defer peer.Close()

	// The peer side is static config, not under test — it just needs to be
	// reachable so the under-test side can complete a real handshake.
	underTestEndpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 61940)
	if err := peer.AddPeer(underTestPub, netip.PrefixFrom(underTestAddr, 32), &underTestEndpoint); err != nil {
		t.Fatalf("configuring peer's view of the node under test: %v", err)
	}

	now := time.Unix(1_800_000_000, 0)
	future := now.Add(time.Hour).Unix()
	resolver := fakeResolver{
		"peer": {Fullname: "peer.acme.eth", Pubkey: strPtr(peerPub), Status: 2, Expiry: itoa(future)},
	}
	peerEndpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 61941)
	loop := &Loop{
		Resolver: resolver,
		Table:    underTest,
		Peers:    []Peer{{Label: "peer", AllowedIP: netip.PrefixFrom(peerAddr, 32), Endpoint: &peerEndpoint}},
		Now:      func() time.Time { return now },
	}

	loop.SyncOnce()
	if !pingUntil(t, underTest, peerAddr, 8*time.Second) {
		t.Fatal("setup failed: node under test never completed a handshake with the authorized peer")
	}

	// Revoke: the resolver now reports the peer as unauthorized, exactly as a
	// cleared `pubkey` text record would (see set-pubkey.ts). No sleeping, no
	// waiting for any rekey timer — just the next sync pass, as Run's ticker
	// would trigger on the next TTL tick.
	resolver["peer"] = &sidecar.DeviceRecord{Fullname: "peer.acme.eth", Pubkey: nil, Status: 0, Expiry: "0"}
	loop.SyncOnce()

	ok, err := underTest.Ping(peerAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("ping errored (want a clean timeout, not an error): %v", err)
	}
	if ok {
		t.Fatal("node under test still completed a handshake with a revoked peer immediately after SyncOnce removed it")
	}

	peers, err := underTest.Peers()
	if err != nil {
		t.Fatalf("reading node-under-test peer table: %v", err)
	}
	for _, p := range peers {
		if p.PublicKeyHex == peerPub {
			t.Fatal("revoked peer is still present in the live WireGuard peer table after SyncOnce")
		}
	}
}
