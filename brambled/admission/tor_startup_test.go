package admission

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

// torSidecar stands in for the real sidecar's routes/tor.ts gate: while
// starting is true every data route answers 503 "starting Tor", exactly as
// requireTorReady does; otherwise it serves record.
type torSidecar struct {
	starting atomic.Bool
	record   atomic.Pointer[sidecar.DeviceRecord]
}

func newTorSidecar(t *testing.T) (*torSidecar, *sidecar.Manager) {
	t.Helper()
	s := &torSidecar{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if s.starting.Load() {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"starting Tor","tor":{"state":"starting"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(s.record.Load())
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing test server port: %v", err)
	}
	return s, &sidecar.Manager{Port: port}
}

// TestSyncOnce_KeepsLastKnownPeersWhileSidecarTorStarts is the W9 startup
// rule (handoff 5.1): while the sidecar's Tor client starts, its data routes
// answer 503, and brambled must keep the peers it already admitted, bounded
// by MaxStale, rather than drop the mesh or admit anything new. Once Tor is
// ready, reads resume and a revocation still takes effect.
func TestSyncOnce_KeepsLastKnownPeersWhileSidecarTorStarts(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future := now.Add(24 * time.Hour).Unix()
	admitted := &sidecar.DeviceRecord{Fullname: "device1.acme.eth", Pubkey: strPtr("abc123"), Status: 2, Expiry: itoa(future)}

	s, manager := newTorSidecar(t)
	s.record.Store(admitted)
	table := newFakeTable()
	clock := now
	loop := &Loop{
		Resolver: manager,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return clock },
		MaxStale: 10 * time.Minute,
	}

	loop.SyncOnce()
	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("setup failed: expected the peer to be admitted while Tor is ready")
	}

	s.starting.Store(true)
	clock = clock.Add(time.Minute)
	loop.SyncOnce()
	if table.removed["abc123"] {
		t.Fatal("peer was removed while the sidecar's Tor client was starting; it must stay for MaxStale")
	}
	if _, ok := table.added["abc123"]; !ok {
		t.Fatal("peer vanished from the table while the sidecar's Tor client was starting")
	}

	s.starting.Store(false)
	revoked := *admitted
	revoked.Revoked = true
	s.record.Store(&revoked)
	clock = clock.Add(time.Minute)
	loop.SyncOnce()
	if !table.removed["abc123"] {
		t.Fatal("expected the revocation to take effect once Tor was ready again")
	}
}

// A node that has never resolved a peer admits nothing while Tor starts:
// the 503 is a resolve error, not an authorization.
func TestSyncOnce_AdmitsNothingWhileSidecarTorStartsOnFirstSync(t *testing.T) {
	s, manager := newTorSidecar(t)
	s.starting.Store(true)
	table := newFakeTable()
	loop := &Loop{
		Resolver: manager,
		Table:    table,
		Peers:    []Peer{{Label: "device1", AllowedIP: netip.MustParsePrefix("10.0.0.1/32")}},
		Now:      func() time.Time { return time.Unix(1_800_000_000, 0) },
		MaxStale: 10 * time.Minute,
	}

	loop.SyncOnce()

	if len(table.added) != 0 {
		t.Fatalf("expected no peer to be admitted without a successful read, got %v", table.added)
	}
}
