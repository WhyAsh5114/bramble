package statusapi

import (
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

type pathObservation struct{ publicKey, state string }
type pathCache struct {
	mu            sync.Mutex
	byLabel       map[string]pathObservation
	endpointState string
}

// OnPathEvent records traversal decisions. It never changes peer admission.
func (s *Server) OnPathEvent(label, publicKey, state string) {
	ev, ok := s.admission.get(label)
	if !ok || !ev.Authorized || ev.PublicKey != publicKey {
		return
	}
	s.paths.mu.Lock()
	if s.paths.byLabel == nil {
		s.paths.byLabel = make(map[string]pathObservation)
	}
	previous := s.paths.byLabel[label]
	next := pathObservation{publicKey, state}
	s.paths.byLabel[label] = next
	s.paths.mu.Unlock()
	if next != previous {
		s.activity.add(ActivityEvent{Time: time.Now(), Kind: "path", Label: label, Outcome: state, Message: "peer path: " + state})
	}
}

// OnEndpointEvent records STUN observation changes, not proof of reachability.
func (s *Server) OnEndpointEvent(status wgnode.EndpointStatus) {
	s.paths.mu.Lock()
	changed := s.paths.endpointState != status.State
	s.paths.endpointState = status.State
	s.paths.mu.Unlock()
	if changed {
		s.activity.add(ActivityEvent{Time: time.Now(), Kind: "stun", Outcome: status.State, Message: "STUN: " + status.State})
	}
}

func (s *Server) path(label, publicKey string) (string, string) {
	s.paths.mu.Lock()
	defer s.paths.mu.Unlock()
	p := s.paths.byLabel[label]
	if p.publicKey != publicKey {
		return "unknown", ""
	}
	switch p.state {
	case "direct":
		return "direct", p.state
	case "relay":
		return "relayed", p.state
	default:
		return "unknown", p.state
	}
}
