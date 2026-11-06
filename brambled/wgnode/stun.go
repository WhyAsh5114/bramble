package wgnode

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// EndpointStatus describes an observation by a STUN server, not proof of peer
// reachability. It is kept only in memory and never published to ENS.
type EndpointStatus struct {
	State      string     `json:"state"`
	Endpoint   string     `json:"endpoint,omitempty"`
	Server     string     `json:"server,omitempty"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type discoveryState struct {
	mu     sync.Mutex
	query  sync.Mutex
	status EndpointStatus
}

func (n *Node) EndpointStatus() EndpointStatus {
	n.discovery.mu.Lock()
	defer n.discovery.mu.Unlock()
	s := n.discovery.status
	if s.State == "" {
		s.State = "disabled"
	}
	if s.ObservedAt != nil {
		t := *s.ObservedAt
		s.ObservedAt = &t
	}
	return s
}

// DiscoverEndpoint runs discovery on the WireGuard bind and updates statusapi's
// snapshot. Calls are serialized; failures clear the previous observation.
func (n *Node) DiscoverEndpoint(ctx context.Context, servers []string) (netip.AddrPort, error) {
	if n.stunBind == nil {
		return netip.AddrPort{}, fmt.Errorf("STUN is disabled; set Config.EnableSTUN")
	}
	n.discovery.query.Lock()
	defer n.discovery.query.Unlock()
	n.discovery.mu.Lock()
	n.discovery.status = EndpointStatus{State: "discovering"}
	n.discovery.mu.Unlock()
	endpoint, server, err := n.stunBind.Discover(ctx, servers)
	n.discovery.mu.Lock()
	defer n.discovery.mu.Unlock()
	if err != nil {
		n.discovery.status = EndpointStatus{State: "unavailable", Error: "STUN discovery failed"}
		return netip.AddrPort{}, err
	}
	now := time.Now().UTC()
	n.discovery.status = EndpointStatus{State: "discovered", Endpoint: endpoint.String(), Server: server, ObservedAt: &now}
	return endpoint, nil
}

// RunSTUN refreshes the observation every 30 seconds after each discovery pass.
// The caller cancels ctx before closing the node.
func (n *Node) RunSTUN(ctx context.Context, servers []string) {
	for ctx.Err() == nil {
		_, _ = n.DiscoverEndpoint(ctx, servers)
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
