package statusapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/gateway"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

type fakePeerTable []wgnode.PeerInfo

func (f fakePeerTable) Peers() ([]wgnode.PeerInfo, error) { return f, nil }

func TestPeersJoinsAdmissionAndWireGuardState(t *testing.T) {
	s := &Server{PeerTable: fakePeerTable{
		{PublicKeyHex: "abc123", AllowedIPs: []string{"10.77.0.2/32"}, LastHandshakeUnixNs: 42, RxBytes: 1000},
	}}
	s.OnAdmissionEvent(admission.Event{Label: "worker", Authorized: true, PublicKey: "abc123"})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/peers", nil))

	var got []PeerStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 peer, got %d: %+v", len(got), got)
	}
	p := got[0]
	if p.Label != "worker" || !p.Authorized || !p.Handshaked || p.RxBytes != 1000 {
		t.Fatalf("peer status didn't join wireguard state correctly: %+v", p)
	}
	if len(p.AllowedIPs) != 1 || p.AllowedIPs[0] != "10.77.0.2/32" {
		t.Fatalf("expected allowed-ip from wireguard state, got %+v", p.AllowedIPs)
	}
}

func TestActivityFeedCollectsAdmissionAndGatewayEvents(t *testing.T) {
	s := &Server{}
	s.OnAdmissionEvent(admission.Event{Label: "worker", Authorized: false})
	s.OnGatewayDecision(gateway.Decision{RequesterLabel: "worker", Service: "web", Allowed: false, Detail: "no matching granted digest"})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/activity", nil))

	var got []ActivityEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 activity events, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "admission" || got[1].Kind != "gateway" {
		t.Fatalf("unexpected event kinds: %+v", got)
	}
}

func TestActivityFeedIsBounded(t *testing.T) {
	s := &Server{}
	for i := 0; i < activityBufferSize+50; i++ {
		s.OnAdmissionEvent(admission.Event{Label: "worker", Authorized: true})
	}
	if got := len(s.activity.snapshot()); got != activityBufferSize {
		t.Fatalf("expected ring buffer capped at %d, got %d", activityBufferSize, got)
	}
}

func TestPingRejectsUnknownLabel(t *testing.T) {
	s := &Server{PeerTable: fakePeerTable{}}

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(pingRequest{Label: "not-a-tracked-peer"})
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/ping", bytes.NewReader(body)))

	if rec.Code != 400 {
		t.Fatalf("expected 400 for an unknown label, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPingReportsUnreachableWhenPeerNotInWireGuardTable(t *testing.T) {
	s := &Server{PeerTable: fakePeerTable{}}
	s.OnAdmissionEvent(admission.Event{Label: "worker", Authorized: false, PublicKey: "abc123"})

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(pingRequest{Label: "worker"})
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/ping", bytes.NewReader(body)))

	var got pingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Reachable {
		t.Fatalf("expected unreachable for a peer not in the wireguard table, got %+v", got)
	}
}

// TestPingRealLoopback exercises the real OS ping subprocess and RTT
// parsing end to end (not a stub) — loopback is the one target that's
// always reachable on any machine this test runs on, on every OS this
// repo targets.
func TestPingRealLoopback(t *testing.T) {
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skip("ping binary not available in this environment")
	}
	s := &Server{PeerTable: fakePeerTable{
		{PublicKeyHex: "abc123", AllowedIPs: []string{"127.0.0.1/32"}},
	}}
	s.OnAdmissionEvent(admission.Event{Label: "worker", Authorized: true, PublicKey: "abc123"})

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(pingRequest{Label: "worker"})
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/ping", bytes.NewReader(body)))

	var got pingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !got.Reachable {
		t.Fatalf("expected loopback to be reachable, got %+v", got)
	}
}
