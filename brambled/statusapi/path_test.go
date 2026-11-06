package statusapi_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/statusapi"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

type pathPeers struct{ peers []wgnode.PeerInfo }

func (p *pathPeers) Peers() ([]wgnode.PeerInfo, error) { return p.peers, nil }

func TestPathEventsExposeCurrentPathAndClearAfterRevocation(t *testing.T) {
	table := &pathPeers{[]wgnode.PeerInfo{{PublicKeyHex: "peer", Endpoint: "192.0.2.1:1234"}}}
	s := &statusapi.Server{PeerTable: table}
	s.OnAdmissionEvent(admission.Event{Label: "bob", PublicKey: "peer", Authorized: true})
	s.OnPathEvent("bob", "peer", "probing")
	s.OnPathEvent("bob", "peer", "direct")
	s.OnPathEvent("bob", "peer", "relay")
	s.OnEndpointEvent(wgnode.EndpointStatus{State: "discovered", Endpoint: "192.0.2.2:1234"})
	read := func() []statusapi.PeerStatus {
		t.Helper()
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/peers", nil))
		var rows []statusapi.PeerStatus
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := read()
	if len(rows) != 1 || rows[0].Path != "relayed" || rows[0].TraversalState != "relay" {
		t.Fatalf("path: %+v", rows)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/activity", nil))
	var events []statusapi.ActivityEvent
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, event := range events {
		if event.Kind == "path" {
			paths = append(paths, event.Outcome)
		}
	}
	if len(paths) != 3 || paths[0] != "probing" || paths[1] != "direct" || paths[2] != "relay" {
		t.Fatalf("events: %+v", events)
	}
	foundSTUN := false
	for _, event := range events {
		if event.Kind == "stun" && event.Outcome == "discovered" {
			foundSTUN = true
		}
	}
	if !foundSTUN {
		t.Fatal("missing STUN discovery event")
	}
	s.OnAdmissionEvent(admission.Event{Label: "bob", PublicKey: "peer", Authorized: false})
	table.peers = nil
	s.OnPathEvent("bob", "peer", "direct") // late controller callback
	rows = read()
	if rows[0].Path != "unknown" {
		t.Fatalf("revoked path: %+v", rows)
	}
}
