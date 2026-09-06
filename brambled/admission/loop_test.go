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
