package main

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

// fakeMeshResolver is a minimal admission.Resolver for resolvePeerMeshIPs'
// tests — real ENS resolution needs a live chain, so this stands in for
// sidecar.Manager the same way admission's own tests fake it out.
type fakeMeshResolver map[string]*sidecar.DeviceRecord

func (f fakeMeshResolver) ResolveDevice(label string) (*sidecar.DeviceRecord, error) {
	record, ok := f[label]
	if !ok {
		return nil, fmt.Errorf("no such device: %s", label)
	}
	return record, nil
}

func meshIPPtr(s string) *string { return &s }

func TestResolvePeerMeshIPs_ResolvesBareLabelFromChain(t *testing.T) {
	resolver := fakeMeshResolver{
		"self":  {MeshIP: meshIPPtr("10.77.0.2/24")},
		"other": {MeshIP: meshIPPtr("10.77.0.5/24")},
	}
	prefix, peers, err := resolvePeerMeshIPs(resolver, "self", "", []admission.Peer{{Label: "other"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prefix.String() != "10.77.0.2/24" {
		t.Errorf("local prefix = %s, want 10.77.0.2/24", prefix)
	}
	// The resolved peer route must be a /32, not the /24 stored on chain —
	// a wider mask here would let this peer's route swallow the rest of
	// the mesh (docs/adr/0009).
	if len(peers) != 1 || peers[0].AllowedIP.String() != "10.77.0.5/32" {
		t.Errorf("peer allowed IP = %v, want 10.77.0.5/32", peers)
	}
}

func TestResolvePeerMeshIPs_ExplicitAllowedIPWinsOverChain(t *testing.T) {
	resolver := fakeMeshResolver{"self": {MeshIP: meshIPPtr("10.77.0.2/24")}}
	explicit := netip.MustParsePrefix("10.77.0.9/32")
	_, peers, err := resolvePeerMeshIPs(resolver, "self", "", []admission.Peer{{Label: "other", AllowedIP: explicit}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if peers[0].AllowedIP != explicit {
		t.Errorf("explicit -peer allowed-ip should be preserved untouched, got %v", peers[0].AllowedIP)
	}
}

func TestResolvePeerMeshIPs_ExplicitLocalAddrWinsOverChain(t *testing.T) {
	resolver := fakeMeshResolver{"self": {MeshIP: meshIPPtr("10.77.0.2/24")}}
	prefix, _, err := resolvePeerMeshIPs(resolver, "self", "10.9.9.9/24", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prefix.String() != "10.9.9.9/24" {
		t.Errorf("explicit -local-addr should win over the chain record, got %s", prefix)
	}
}

func TestResolvePeerMeshIPs_MissingMeshIPRecordErrors(t *testing.T) {
	resolver := fakeMeshResolver{"self": {MeshIP: nil}}
	if _, _, err := resolvePeerMeshIPs(resolver, "self", "", nil); err == nil {
		t.Fatal("expected an error for a device with no mesh-ip record and no -local-addr override")
	}
}

func TestResolvePeerMeshIPs_CollisionIsRejected(t *testing.T) {
	resolver := fakeMeshResolver{
		"self":  {MeshIP: meshIPPtr("10.77.0.2/24")},
		"other": {MeshIP: meshIPPtr("10.77.0.2/24")}, // same host as self
	}
	if _, _, err := resolvePeerMeshIPs(resolver, "self", "", []admission.Peer{{Label: "other"}}); err == nil {
		t.Fatal("expected a collision error when two resolved addresses match")
	}
}

func TestPeerFlagSet_BareLabelLeavesAllowedIPZero(t *testing.T) {
	var peers peerFlag
	if err := peers.Set("device2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 1 || peers[0].Label != "device2" || peers[0].AllowedIP.IsValid() {
		t.Errorf("got %+v, want a bare label with zero AllowedIP", peers)
	}
}

func TestPeerFlagSet_ExplicitFormStillWorks(t *testing.T) {
	var peers peerFlag
	if err := peers.Set("device2=10.77.0.5/32"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 1 || peers[0].Label != "device2" || peers[0].AllowedIP.String() != "10.77.0.5/32" {
		t.Errorf("got %+v, want the explicit label=ip/prefix form parsed as before", peers)
	}
}
