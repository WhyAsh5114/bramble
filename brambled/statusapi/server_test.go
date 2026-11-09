package statusapi

import (
	"bytes"
	"encoding/json"
	"errors"
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
		{PublicKeyHex: "abc123", AllowedIPs: []string{"10.77.0.2/32"}, LastHandshakeUnixNs: 42, RxBytes: 1000, Endpoint: "203.0.113.7:51820"},
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
	if p.Endpoint != "203.0.113.7:51820" {
		t.Fatalf("expected the wireguard endpoint, got %q", p.Endpoint)
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
		s.OnGatewayDecision(gateway.Decision{RequesterLabel: "worker", Service: "web", Allowed: true, Detail: "proxying"})
	}
	if got := len(s.activity.snapshot()); got != activityBufferSize {
		t.Fatalf("expected ring buffer capped at %d, got %d", activityBufferSize, got)
	}
}

func TestActivityFeedRecordsAdmissionOnlyOnChange(t *testing.T) {
	s := &Server{}
	for i := 0; i < 5; i++ {
		s.OnAdmissionEvent(admission.Event{Label: "worker", Authorized: true, PublicKey: "abc"})
	}
	if got := len(s.activity.snapshot()); got != 1 {
		t.Fatalf("five identical sync passes must record one event, got %d: %+v", got, s.activity.snapshot())
	}
}

func TestActivityFeedReportsPeerDroppedOnce(t *testing.T) {
	s := &Server{}
	s.OnAdmissionEvent(admission.Event{Label: "agent-1", Authorized: true, PublicKey: "abc"})
	// A resolver error says nothing about admission, so it must not look like a drop.
	s.OnAdmissionEvent(admission.Event{Label: "agent-1", Authorized: false, Err: errors.New("rpc timeout")})
	s.OnAdmissionEvent(admission.Event{Label: "agent-1", Authorized: false})
	s.OnAdmissionEvent(admission.Event{Label: "agent-1", Authorized: false})

	var dropped []ActivityEvent
	for _, e := range s.activity.snapshot() {
		if e.Kind == "peer" {
			dropped = append(dropped, e)
		}
	}
	if len(dropped) != 1 || dropped[0].Label != "agent-1" || dropped[0].Outcome != "dropped" {
		t.Fatalf("expected exactly one dropped event for agent-1, got %+v", dropped)
	}
}

func TestActivityFeedDoesNotReportDropForFailedRemoval(t *testing.T) {
	s := &Server{}
	s.OnAdmissionEvent(admission.Event{Label: "agent-1", Authorized: true, PublicKey: "abc"})
	s.OnAdmissionEvent(admission.Event{Label: "agent-1", Authorized: false, Err: errors.New("removing peer abc: failed")})
	for _, e := range s.activity.snapshot() {
		if e.Kind == "peer" {
			t.Fatalf("a failed removal left the peer admitted, but the feed reported a drop: %+v", e)
		}
	}
}

func TestGatewayActivityCarriesOutcomeAndService(t *testing.T) {
	s := &Server{}
	s.OnGatewayDecision(gateway.Decision{RequesterLabel: "agent-1", Service: "staging-tasks", Allowed: false, Detail: "no matching granted digest"})
	s.OnGatewayDecision(gateway.Decision{RequesterLabel: "agent-1", Service: "staging-tasks", Allowed: true, Detail: "proxying to 127.0.0.1:9000"})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/activity", nil))
	var got []ActivityEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(got) != 2 || got[0].Outcome != "denied" || got[1].Outcome != "allowed" ||
		got[0].Service != "staging-tasks" || got[1].Service != "staging-tasks" {
		t.Fatalf("gateway events must carry outcome and service: %+v", got)
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

func TestRelaysReportsTheDataRelayPool(t *testing.T) {
	s := &Server{DataRelays: func() map[string]string { return map[string]string{"relay-eu": "http://203.0.113.9:8080"} }}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/relays", nil))
	var got struct {
		DataRelays map[string]string `json:"dataRelays"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.DataRelays["relay-eu"] != "http://203.0.113.9:8080" {
		t.Fatalf("expected the relay pool, got %+v", got)
	}
}
