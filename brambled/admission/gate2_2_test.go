package admission

import (
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

// TestGate2_2_FreshLoopReadsCurrentChainStateNotPriorMemory is Gate 2.2's
// automated evidence (docs/05_BUILD_PLAN.md): "delete every local cache,
// restart both nodes, confirm the mesh reassembles purely from chain
// state." The structural half of that property is what scripts/
// check-no-local-state.mjs guards (the demo path has no file writes at
// all — there is no cache to delete); this test guards the behavioral half:
// a Loop constructed with zero prior state (the "restarted node") admits
// exactly what the resolver — the chain stand-in — reports *now*, with no
// memory of what it reported before the restart.
//
// The Loop does keep in-process state across its own syncs (knownPubkeys,
// resolvedEndpoints, lastSuccess — see loop.go), but that is revocation
// bookkeeping re-derived from chain state on every sync, never persisted:
// the only thing that survives a restart is what's on chain. That's what
// the two fresh-Loop phases below assert.
//
// The gateway side of the gate needs no test here: CheckACL resolves fresh
// chain state on every CONNECT — "no cache to invalidate" is adr/0006's
// live-verified Sept 8 property (clearing an on-chain acl record turned the
// next request into a denial with no process restart).
func TestGate2_2_FreshLoopReadsCurrentChainStateNotPriorMemory(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future := now.Add(24 * time.Hour).Unix()

	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("pre-restart-key"), Status: 2, Expiry: itoa(future)},
	}

	// Phase 1: the pre-restart node syncs against the current chain state
	// and admits the peer under the key it sees there.
	preRestartTable := newFakeTable()
	preRestartLoop := &Loop{
		Resolver: resolver,
		Table:    preRestartTable,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return now },
	}
	preRestartLoop.SyncOnce()
	if _, ok := preRestartTable.added["pre-restart-key"]; !ok {
		t.Fatal("setup failed: expected the pre-restart node to admit the peer under its chain-reported key")
	}

	// The cache wipe + restart: a brand-new Loop and a brand-new peer table,
	// sharing nothing with the old node — while the chain rotated the label
	// to a different key in between.
	resolver["device1"] = &sidecar.DeviceRecord{Fullname: "device1.acme.eth", Pubkey: strPtr("post-restart-key"), Status: 2, Expiry: itoa(future)}

	restartedTable := newFakeTable()
	endpointCalls := 0
	restartedLoop := &Loop{
		Resolver: resolver,
		Table:    restartedTable,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return now },
		EndpointResolver: func(Peer, string) (*netip.AddrPort, error) {
			endpointCalls++
			return nil, nil
		},
	}
	restartedLoop.SyncOnce()

	// The restarted node must admit the *current* chain key — and nothing
	// of the pre-restart key: no persisted memory carried it over, so the
	// old key must not appear in the fresh peer table.
	if _, ok := restartedTable.added["post-restart-key"]; !ok {
		t.Fatal("expected the restarted node to admit the peer under its current chain-reported key")
	}
	if _, ok := restartedTable.added["pre-restart-key"]; ok {
		t.Fatal("the pre-restart key leaked into the restarted node's peer table — the restarted node must derive its peer set purely from current chain state")
	}
	// Endpoint discovery must be consulted afresh too — a restarted node has
	// no "already resolved this peer's endpoint" memory to skip the lookup.
	if endpointCalls != 1 {
		t.Fatalf("expected the restarted node to consult the endpoint resolver once, got %d calls", endpointCalls)
	}

	// Phase 3: revocation after the restart — the chain clears the pubkey.
	// The restarted node must still remove the peer, via its own
	// in-process memory of what the chain said *since* the restart; the
	// pre-restart node's memory plays no part.
	resolver["device1"] = &sidecar.DeviceRecord{Fullname: "device1.acme.eth", Pubkey: nil, Status: 0, Expiry: "0"}
	restartedLoop.SyncOnce()

	if !restartedTable.removed["post-restart-key"] {
		t.Fatal("expected the restarted node to remove the peer once the chain cleared its pubkey — the clear-pubkey revocation path must survive a restart")
	}
}
