package main

import (
	"fmt"
	"testing"
)

// fakeDeviceLister is a minimal deviceLister for discoverAllPeers' tests —
// real discovery needs a live sidecar/chain, so this stands in for
// sidecar.Manager the same way fakeMeshResolver does for admission.Resolver.
type fakeDeviceLister struct {
	labels []string
	err    error
}

func (f fakeDeviceLister) ListDevices() ([]string, error) { return f.labels, f.err }

func TestDiscoverAllPeers_ExcludesSelf(t *testing.T) {
	lister := fakeDeviceLister{labels: []string{"self", "gateway", "worker"}}
	peers, err := discoverAllPeers(lister, "self")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers, want 2 (self excluded): %+v", len(peers), peers)
	}
	labels := map[string]bool{}
	for _, p := range peers {
		labels[p.Label] = true
	}
	if !labels["gateway"] || !labels["worker"] {
		t.Errorf("got %+v, want gateway and worker present", peers)
	}
}

func TestDiscoverAllPeers_EveryPeerStartsBareForMeshIPResolution(t *testing.T) {
	lister := fakeDeviceLister{labels: []string{"self", "gateway"}}
	peers, err := discoverAllPeers(lister, "self")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 1 || peers[0].AllowedIP.IsValid() {
		t.Errorf("got %+v, want one bare peer with zero AllowedIP for resolvePeerMeshIPs to fill in", peers)
	}
}

func TestDiscoverAllPeers_PropagatesListError(t *testing.T) {
	lister := fakeDeviceLister{err: fmt.Errorf("sidecar unreachable")}
	if _, err := discoverAllPeers(lister, "self"); err == nil {
		t.Fatal("expected an error when the sidecar's device list call fails")
	}
}

func TestDiscoverAllPeers_EmptyTailnetYieldsNoPeers(t *testing.T) {
	lister := fakeDeviceLister{labels: []string{"self"}}
	peers, err := discoverAllPeers(lister, "self")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("got %+v, want zero peers when self is the only registered device", peers)
	}
}
