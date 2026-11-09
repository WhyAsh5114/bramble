package brambled

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/registry"
)

// liveTailnet is a fake sidecar whose device list and records can change
// while serve's goroutines read them, the way the chain changes when a
// device is enrolled after serve started.
type liveTailnet struct {
	mu      sync.Mutex
	labels  []string
	records map[string]*registry.DeviceRecord
}

func (n *liveTailnet) ListDevices() ([]string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.labels...), nil
}

func (n *liveTailnet) ResolveDevice(label string) (*registry.DeviceRecord, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	record, ok := n.records[label]
	if !ok {
		return nil, fmt.Errorf("no such device: %s", label)
	}
	copied := *record
	return &copied, nil
}

func (n *liveTailnet) enroll(label, pubkey, meshIP string, revoked bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.labels = append(n.labels, label)
	n.records[label] = &registry.DeviceRecord{
		Pubkey:  &pubkey,
		MeshIP:  &meshIP,
		Expiry:  strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10),
		Revoked: revoked,
	}
}

// syncTable is a concurrency-safe admission.PeerTable.
type syncTable struct {
	mu    sync.Mutex
	added map[string]netip.Prefix
}

func (s *syncTable) AddPeer(pubkey string, allowedIP netip.Prefix, _ *netip.AddrPort) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.added[pubkey] = allowedIP
	return nil
}

func (s *syncTable) RemovePeer(pubkey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.added, pubkey)
	return nil
}

func (s *syncTable) has(pubkey string) (netip.Prefix, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.added[pubkey]
	return p, ok
}

// TestRediscovery_DeviceEnrolledAfterStartupIsAdmitted is the beat 2 case:
// the gateway starts, then a new device is approved. Without a restart the
// gateway must start tracking it, admit it to the WireGuard table, and map
// its tunnel address to its label for the gateway's ACL check.
func TestRediscovery_DeviceEnrolledAfterStartupIsAdmitted(t *testing.T) {
	tailnet := &liveTailnet{records: map[string]*registry.DeviceRecord{}}
	tailnet.enroll("gateway", "gwkey", "10.77.0.1/24", false)
	tailnet.enroll("laptop", "laptopkey", "10.77.0.2/24", false)

	startup, err := discoverAllPeers(tailnet, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	prefix, peers, err := resolvePeerMeshIPs(tailnet, "gateway", "", startup, true)
	if err != nil {
		t.Fatal(err)
	}
	table := &syncTable{added: map[string]netip.Prefix{}}
	// TTL is far longer than the test: the new peer must be admitted by
	// AddPeers' early sync, not by a TTL tick.
	loop := &admission.Loop{Resolver: tailnet, Table: table, Peers: peers, TTL: time.Hour}
	tracker := newPeerTracker("gateway", prefix.Addr(), peers)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)
	var logs bytes.Buffer
	var logMu sync.Mutex
	go rediscoverPeers(ctx, 10*time.Millisecond, tracker, tailnet, tailnet, &lockedWriter{w: &logs, mu: &logMu}, func(added []admission.Peer) {
		loop.AddPeers(added)
	})

	waitFor(t, func() bool { _, ok := table.has("laptopkey"); return ok }, "startup peer admitted")
	if _, ok := tracker.lookup(netip.MustParseAddr("10.77.0.3")); ok {
		t.Fatal("10.77.0.3 mapped to a label before any device holds it")
	}

	tailnet.enroll("phone", "phonekey", "10.77.0.3/24", false)

	waitFor(t, func() bool { _, ok := table.has("phonekey"); return ok }, "late-enrolled peer admitted")
	if got, _ := table.has("phonekey"); got != netip.MustParsePrefix("10.77.0.3/32") {
		t.Fatalf("phone allowed IP = %s, want 10.77.0.3/32", got)
	}
	if label, ok := tracker.lookup(netip.MustParseAddr("10.77.0.3")); !ok || label != "phone" {
		t.Fatalf("gateway lookup of 10.77.0.3 = %q, %v; want phone, true", label, ok)
	}
}

// TestRediscovery_LateRevokedDeviceIsTrackedButNotAdmitted checks that
// rediscovery does not bypass admission: a device that shows up in the
// registry already revoked is tracked, and admission.Loop keeps it out.
func TestRediscovery_LateRevokedDeviceIsTrackedButNotAdmitted(t *testing.T) {
	tailnet := &liveTailnet{records: map[string]*registry.DeviceRecord{}}
	tailnet.enroll("gateway", "gwkey", "10.77.0.1/24", false)
	table := &syncTable{added: map[string]netip.Prefix{}}
	var events sync.Map
	loop := &admission.Loop{
		Resolver: tailnet, Table: table, TTL: time.Hour,
		OnEvent: func(e admission.Event) { events.Store(e.Label, e.Authorized) },
	}
	tracker := newPeerTracker("gateway", netip.MustParseAddr("10.77.0.1"), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)
	go rediscoverPeers(ctx, 10*time.Millisecond, tracker, tailnet, tailnet, &lockedWriter{w: &bytes.Buffer{}, mu: &sync.Mutex{}}, func(added []admission.Peer) {
		loop.AddPeers(added)
	})

	tailnet.enroll("stolen", "stolenkey", "10.77.0.9/24", true)

	waitFor(t, func() bool { _, ok := events.Load("stolen"); return ok }, "revoked peer synced")
	if authorized, _ := events.Load("stolen"); authorized.(bool) {
		t.Fatal("revoked late device reported authorized")
	}
	if _, ok := table.has("stolenkey"); ok {
		t.Fatal("revoked late device was added to the WireGuard table")
	}
}

func TestPeerTracker_DiscoverNew_SkipsUntilMeshIPAndCollisionsLogOnce(t *testing.T) {
	tailnet := &liveTailnet{records: map[string]*registry.DeviceRecord{}}
	tailnet.enroll("gateway", "gwkey", "10.77.0.1/24", false)
	tailnet.enroll("laptop", "laptopkey", "10.77.0.2/24", false)
	tracker := newPeerTracker("gateway", netip.MustParseAddr("10.77.0.1"), []admission.Peer{
		{Label: "laptop", AllowedIP: netip.MustParsePrefix("10.77.0.2/32")},
	})

	// Enrolled, but its mesh-ip record is not written yet.
	tailnet.enroll("phone", "phonekey", "", false)
	// Claims an address the laptop already holds.
	tailnet.enroll("dup", "dupkey", "10.77.0.2/24", false)

	var logs bytes.Buffer
	for i := 0; i < 3; i++ {
		added, err := tracker.discoverNew(tailnet, tailnet, &logs)
		if err != nil {
			t.Fatal(err)
		}
		if len(added) != 0 {
			t.Fatalf("pass %d added %v, want none", i, added)
		}
	}
	if n := strings.Count(logs.String(), `"phone" skipped`); n != 1 {
		t.Fatalf("phone skip logged %d times, want once:\n%s", n, logs.String())
	}
	if n := strings.Count(logs.String(), `"dup" skipped for now: mesh IP collision with laptop`); n != 1 {
		t.Fatalf("dup collision logged %d times, want once:\n%s", n, logs.String())
	}

	// The mesh-ip record lands; the next pass picks the phone up.
	tailnet.mu.Lock()
	ip := "10.77.0.3/24"
	tailnet.records["phone"].MeshIP = &ip
	tailnet.mu.Unlock()

	added, err := tracker.discoverNew(tailnet, tailnet, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].Label != "phone" || added[0].AllowedIP != netip.MustParsePrefix("10.77.0.3/32") {
		t.Fatalf("added = %v, want phone at 10.77.0.3/32", added)
	}
	if again, _ := tracker.discoverNew(tailnet, tailnet, &logs); len(again) != 0 {
		t.Fatalf("phone added twice: %v", again)
	}
	if _, ok := tracker.lookup(netip.MustParseAddr("10.77.0.1")); ok {
		t.Fatal("lookup returned this node's own address as a peer")
	}
}

func TestRediscoverPeers_ListingErrorIsRetried(t *testing.T) {
	lister := &flakyLister{failures: 2, labels: []string{"gateway", "phone"}}
	resolver := fakeMeshResolver{"phone": {MeshIP: meshIPPtr("10.77.0.3/24")}}
	tracker := newPeerTracker("gateway", netip.MustParseAddr("10.77.0.1"), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan []admission.Peer, 1)
	go rediscoverPeers(ctx, 5*time.Millisecond, tracker, lister, resolver, &lockedWriter{w: &bytes.Buffer{}, mu: &sync.Mutex{}}, func(added []admission.Peer) {
		got <- added
	})

	select {
	case added := <-got:
		if len(added) != 1 || added[0].Label != "phone" {
			t.Fatalf("added = %v, want phone", added)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rediscovery never recovered from listing errors")
	}
}

type flakyLister struct {
	mu       sync.Mutex
	failures int
	labels   []string
}

func (f *flakyLister) ListDevices() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures > 0 {
		f.failures--
		return nil, fmt.Errorf("sidecar returned 502: rate limited")
	}
	return f.labels, nil
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}
