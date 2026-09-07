package admission

import (
	"fmt"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

type fakeResolver map[string]*sidecar.DeviceRecord

func (f fakeResolver) ResolveDevice(label string) (*sidecar.DeviceRecord, error) {
	return f[label], nil
}

// erroringResolver lets a test simulate resolver failures (sidecar down,
// RPC unreachable) for specific labels, independent of what fakeResolver
// would otherwise return.
type erroringResolver struct {
	records map[string]*sidecar.DeviceRecord
	failing map[string]bool
}

func (f *erroringResolver) ResolveDevice(label string) (*sidecar.DeviceRecord, error) {
	if f.failing[label] {
		return nil, fmt.Errorf("resolver unavailable")
	}
	return f.records[label], nil
}

type fakeTable struct {
	added   map[string]netip.Prefix
	removed map[string]bool
}

func newFakeTable() *fakeTable {
	return &fakeTable{added: map[string]netip.Prefix{}, removed: map[string]bool{}}
}

func (f *fakeTable) AddPeer(publicKeyHex string, allowedIP netip.Prefix, _ *netip.AddrPort) error {
	f.added[publicKeyHex] = allowedIP
	delete(f.removed, publicKeyHex)
	return nil
}

func (f *fakeTable) RemovePeer(publicKeyHex string) error {
	f.removed[publicKeyHex] = true
	delete(f.added, publicKeyHex)
	return nil
}

func strPtr(s string) *string { return &s }

func TestSyncOnce_AddsAuthorizedPeer(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future := now.Add(24 * time.Hour).Unix()

	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
	}
	table := newFakeTable()
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return now },
	}

	loop.SyncOnce()

	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("expected authorized peer to be added")
	}
}

func TestSyncOnce_RemovesExpiredPeer(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	past := now.Add(-24 * time.Hour).Unix()

	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(past)},
	}
	table := newFakeTable()
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return now },
	}

	loop.SyncOnce()

	if !table.removed["abc123"] {
		t.Fatal("expected expired peer to be removed")
	}
}

// TestSyncOnce_RemovesPeerWhosePubkeyWasCleared covers the revocation shape
// TestSyncOnce_RemovesExpiredPeer doesn't: a device that revokes by clearing
// its own `pubkey` text record (what scripts/provision-dev-tailnet/set-pubkey.ts
// actually does on-chain for Gate 1.3), rather than letting its expiry lapse.
// The record reports no pubkey at all in this case — SyncOnce must remember
// the last pubkey this label was authorized under to remove it (see
// knownPubkeys in loop.go), since the current record can no longer supply it.
func TestSyncOnce_RemovesPeerWhosePubkeyWasCleared(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future := now.Add(time.Hour).Unix()

	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
	}
	table := newFakeTable()
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return now },
	}

	loop.SyncOnce()
	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("setup failed: expected the peer to be added while authorized")
	}

	// Revoke by clearing pubkey — the record now reports none at all, not
	// just an expired one.
	resolver["device1"] = &sidecar.DeviceRecord{Fullname: "device1.acme.eth", Pubkey: nil, Status: 0, Expiry: "0"}
	loop.SyncOnce()

	if !table.removed["abc123"] {
		t.Fatal("expected the peer to be removed after its pubkey was cleared, even though the current record no longer reports that pubkey")
	}
}

func TestSyncOnce_NeverRegisteredPeerIsNeitherAddedNorRemoved(t *testing.T) {
	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: nil, Status: 0, Expiry: "0"},
	}
	table := newFakeTable()
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
	}

	loop.SyncOnce()

	if len(table.added) != 0 || len(table.removed) != 0 {
		t.Fatalf("expected no table mutation for an unregistered label, got added=%v removed=%v", table.added, table.removed)
	}
}

func TestSyncOnce_StaticEndpointWinsOverResolver(t *testing.T) {
	future := time.Unix(1_800_000_000, 0).Add(time.Hour).Unix()
	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
	}
	table := newFakeTable()
	staticEndpoint := netip.MustParseAddrPort("198.51.100.1:51820")
	resolverCalled := false
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32"), Endpoint: &staticEndpoint}},
		Now:      func() time.Time { return time.Unix(1_800_000_000, 0) },
		EndpointResolver: func(Peer, string) (*netip.AddrPort, error) {
			resolverCalled = true
			return nil, nil
		},
	}

	loop.SyncOnce()

	if resolverCalled {
		t.Fatal("EndpointResolver should not be consulted when Peer.Endpoint is set")
	}
	if got := table.added["abc123"]; got != netip.MustParsePrefix("10.0.0.1/32") {
		t.Fatalf("unexpected allowed IP recorded: %v", got)
	}
}

func TestSyncOnce_ResolverConsultedOnceThenCached(t *testing.T) {
	future := time.Unix(1_800_000_000, 0).Add(time.Hour).Unix()
	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
	}
	table := newFakeTable()
	resolvedEndpoint := netip.MustParseAddrPort("203.0.113.5:51820")
	calls := 0
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return time.Unix(1_800_000_000, 0) },
		EndpointResolver: func(Peer, string) (*netip.AddrPort, error) {
			calls++
			return &resolvedEndpoint, nil
		},
	}

	loop.SyncOnce()
	loop.SyncOnce()
	loop.SyncOnce()

	if calls != 1 {
		t.Fatalf("expected EndpointResolver to be consulted exactly once, got %d calls", calls)
	}
}

func TestSyncOnce_ResolverErrorDoesNotBlockAdmission(t *testing.T) {
	future := time.Unix(1_800_000_000, 0).Add(time.Hour).Unix()
	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
	}
	table := newFakeTable()
	var events []Event
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return time.Unix(1_800_000_000, 0) },
		EndpointResolver: func(Peer, string) (*netip.AddrPort, error) {
			return nil, fmt.Errorf("relay unreachable")
		},
		OnEvent: func(e Event) { events = append(events, e) },
	}

	loop.SyncOnce()

	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("expected the peer to still be added despite the endpoint resolver failing")
	}
	if len(events) != 1 || events[0].EndpointErr == nil {
		t.Fatalf("expected EndpointErr to be surfaced on the event, got: %+v", events)
	}
}

// TestSyncOnce_KeyRotationRemovesOldPeer is a regression test for a bug
// found reviewing the admission loop: a label whose pubkey changes while
// remaining authorized (rotation, not revocation) used to be added under
// the new key without ever removing the old one. Because wgnode's IpcSet
// keys peers by public key, the old key stayed in the live peer table
// forever — a real hole under 02_TRACK_FIT.md's "a device can rotate its
// own key" pitch line.
func TestSyncOnce_KeyRotationRemovesOldPeer(t *testing.T) {
	future := time.Unix(1_800_000_000, 0).Add(time.Hour).Unix()
	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("old-key"), Status: 2, Expiry: itoa(future)},
	}
	table := newFakeTable()
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return time.Unix(1_800_000_000, 0) },
	}

	loop.SyncOnce()
	if _, ok := table.added["old-key"]; !ok {
		t.Fatal("setup failed: expected the peer to be added under its original key")
	}

	// Rotate: same label, still authorized, different pubkey.
	resolver["device1"] = &sidecar.DeviceRecord{Fullname: "device1.acme.eth", Pubkey: strPtr("new-key"), Status: 2, Expiry: itoa(future)}
	loop.SyncOnce()

	if !table.removed["old-key"] {
		t.Fatal("expected the old key to be removed from the peer table after rotation")
	}
	if _, ok := table.added["new-key"]; !ok {
		t.Fatal("expected the new key to be added after rotation")
	}
}

// TestSyncOnce_ResolverErrorLeavesPeerAdmittedWithoutMaxStale documents the
// default fail-open behavior: with MaxStale unset (zero), a resolver error
// never revokes an already-admitted peer, no matter how long resolution
// keeps failing.
func TestSyncOnce_ResolverErrorLeavesPeerAdmittedWithoutMaxStale(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future := now.Add(time.Hour).Unix()
	resolver := &erroringResolver{
		records: map[string]*sidecar.DeviceRecord{
			"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
		},
		failing: map[string]bool{},
	}
	table := newFakeTable()
	clock := now
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return clock },
	}

	loop.SyncOnce()
	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("setup failed: expected the peer to be added while authorized")
	}

	resolver.failing["device1"] = true
	clock = clock.Add(24 * time.Hour)
	loop.SyncOnce()

	if table.removed["abc123"] {
		t.Fatal("expected the peer to remain admitted on resolver error when MaxStale is unset (fail-open default)")
	}
}

// TestSyncOnce_MaxStaleRemovesPeerAfterSustainedResolverFailure is the
// regression test for the fail-open finding: MaxStale bounds how long a
// resolver error can leave an already-admitted peer in place. Below the
// bound the peer must stay (a transient RPC hiccup shouldn't tear down a
// legitimate connection); past the bound it must be removed.
func TestSyncOnce_MaxStaleRemovesPeerAfterSustainedResolverFailure(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future := now.Add(time.Hour).Unix()
	resolver := &erroringResolver{
		records: map[string]*sidecar.DeviceRecord{
			"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
		},
		failing: map[string]bool{},
	}
	table := newFakeTable()
	clock := now
	loop := &Loop{
		Resolver: resolver,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return clock },
		MaxStale: 10 * time.Second,
	}

	loop.SyncOnce()
	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("setup failed: expected the peer to be added while authorized")
	}

	resolver.failing["device1"] = true

	// Still within MaxStale — must remain fail-open.
	clock = clock.Add(5 * time.Second)
	loop.SyncOnce()
	if table.removed["abc123"] {
		t.Fatal("expected the peer to remain admitted before MaxStale elapses")
	}

	// Past MaxStale — must flip to fail-closed.
	clock = clock.Add(10 * time.Second)
	loop.SyncOnce()
	if !table.removed["abc123"] {
		t.Fatal("expected the peer to be removed once the resolver error outlasted MaxStale")
	}

	// Recovery: resolution succeeds again, peer should be re-admitted
	// cleanly (not blocked by stale internal state from the removal).
	resolver.failing["device1"] = false
	clock = clock.Add(time.Second)
	loop.SyncOnce()
	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("expected the peer to be re-admitted once resolution recovers")
	}
}

func TestSyncOnce_EmitsEventPerPeer(t *testing.T) {
	future := time.Unix(1_800_000_000, 0).Add(time.Hour).Unix()
	resolver := fakeResolver{
		"device1": {Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)},
	}
	var events []Event
	loop := &Loop{
		Resolver: resolver,
		Table:    newFakeTable(),
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return time.Unix(1_800_000_000, 0) },
		OnEvent:  func(e Event) { events = append(events, e) },
	}

	loop.SyncOnce()

	if len(events) != 1 || !events[0].Authorized || events[0].Label != "device1" {
		t.Fatalf("unexpected events: %+v", events)
	}
}
