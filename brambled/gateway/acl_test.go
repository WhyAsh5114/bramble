package gateway

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

// fakeResolver is an in-memory stand-in for *sidecar.Manager, keyed by
// label, for testing CheckACL without a live sidecar/chain.
type fakeResolver map[string]*sidecar.DeviceRecord

func (f fakeResolver) ResolveDevice(label string) (*sidecar.DeviceRecord, error) {
	rec, ok := f[label]
	if !ok {
		return nil, fmt.Errorf("no such device: %s", label)
	}
	return rec, nil
}

func strPtr(s string) *string { return &s }

func mustPub(t *testing.T, privHex string) string {
	t.Helper()
	pub, err := publicFromPrivate(privHex)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func mustDigest(t *testing.T, granterPriv, gatewayPub, service string) string {
	t.Helper()
	d, err := ACLDigestECDH(granterPriv, gatewayPub, service)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestCheckACL_TrustedGranterVouchesForDevice mirrors adr/0005's worked
// example: granter A vouches for device D to reach service "db" on gateway
// G. G trusts A (A is in G's acl-granters). D's acl record holds the digest
// A computed for G. G should independently derive the same digest and allow.
func TestCheckACL_TrustedGranterVouchesForDevice(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)

	digest := mustDigest(t, granterPriv, gatewayPub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digest}},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected trusted granter's digest to be accepted")
	}
}

// TestCheckACL_UntrustedGranterIsIgnored: the device's acl digest was
// produced by a real granter identity, but the gateway doesn't list that
// granter in its own acl-granters — the digest must never match, because a
// gateway's trust decision is its own, not the device's.
func TestCheckACL_UntrustedGranterIsIgnored(t *testing.T) {
	untrustedGranterPriv := strings.Repeat("cc", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)

	digest := mustDigest(t, untrustedGranterPriv, gatewayPub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"someone-else"}}, // untrustedGranterPriv's label is never listed
		"device1":  {ACL: []string{digest}},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected an untrusted granter's digest to be rejected")
	}
}

// TestCheckACL_WrongServiceNameDenied: a digest for "db" doesn't authorize
// "cache" — HMAC over a different message produces an unrelated digest.
func TestCheckACL_WrongServiceNameDenied(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)

	digest := mustDigest(t, granterPriv, gatewayPub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digest}},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "cache")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a db-only grant to be denied for cache")
	}
}

// TestCheckACL_DifferentGatewayDenied: the same granter vouching for the
// same device+service on a *different* gateway produces a different digest
// (the ECDH pair is per-gateway) — digests are not portable across gateways.
// This is the second scenario docs/05_BUILD_PLAN.md Gate 2.3 names
// ("CONNECT db on a different host entirely").
func TestCheckACL_DifferentGatewayDenied(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gateway1Priv := strings.Repeat("bb", 32)
	gateway2Priv := strings.Repeat("dd", 32)
	gateway1Pub := mustPub(t, gateway1Priv)

	digestForGateway1 := mustDigest(t, granterPriv, gateway1Pub, "db")

	resolver := fakeResolver{
		"gateway2": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digestForGateway1}},
	}

	ok, err := CheckACL(resolver, gateway2Priv, "gateway2", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a digest computed for gateway1 to be rejected by gateway2")
	}
}

func TestCheckACL_DeviceWithNoACLDenied(t *testing.T) {
	gatewayPriv := strings.Repeat("bb", 32)
	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, strings.Repeat("aa", 32)))},
		"device1":  {ACL: nil},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a device with an empty acl to be denied")
	}
}

func TestCheckACL_UnresolvableGranterIsSkippedNotFatal(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)

	digest := mustDigest(t, granterPriv, gatewayPub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"ghost-granter", "granter1"}}, // ghost-granter doesn't resolve
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digest}},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected an unresolvable granter to be skipped, not to block a later valid one")
	}
}
