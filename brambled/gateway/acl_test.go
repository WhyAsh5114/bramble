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

// device1Priv/device1Pub is the requester identity every test below uses
// unless it's specifically testing the requester-binding itself — every
// digest now has to be computed for a specific requester pubkey (see
// digest.go's doc comment), so tests need a real one, not just a device
// label.
var device1Priv = strings.Repeat("22", 32)

func mustDigest(t *testing.T, granterPriv, gatewayPub, requesterPub, service string) string {
	t.Helper()
	d, err := ACLDigestECDH(granterPriv, gatewayPub, requesterPub, service)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestCheckACL_TrustedGranterVouchesForDevice mirrors adr/0005's worked
// example: granter A vouches for device D to reach service "db" on gateway
// G. G trusts A (A is in G's acl-granters). D's acl record holds the digest
// A computed for G, bound to D's own pubkey. G should independently derive
// the same digest and allow.
func TestCheckACL_TrustedGranterVouchesForDevice(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)
	device1Pub := mustPub(t, device1Priv)

	digest := mustDigest(t, granterPriv, gatewayPub, device1Pub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digest}, Pubkey: strPtr(device1Pub)},
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
	device1Pub := mustPub(t, device1Priv)

	digest := mustDigest(t, untrustedGranterPriv, gatewayPub, device1Pub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"someone-else"}}, // untrustedGranterPriv's label is never listed
		"device1":  {ACL: []string{digest}, Pubkey: strPtr(device1Pub)},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected an untrusted granter's digest to be rejected")
	}
}

// TestCheckACL_RevokedGranterIsIgnored: the granter's digest is otherwise
// valid and the granter is still listed in acl-granters, but the granter
// identity itself has been revoked — its past vouches must stop counting,
// not remain valid forever.
func TestCheckACL_RevokedGranterIsIgnored(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)
	device1Pub := mustPub(t, device1Priv)

	digest := mustDigest(t, granterPriv, gatewayPub, device1Pub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv)), Revoked: true},
		"device1":  {ACL: []string{digest}, Pubkey: strPtr(device1Pub)},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a revoked granter's digest to be rejected")
	}
}

// TestCheckACL_DigestCopiedToAnotherDeviceDenied is the bug this session's
// review found and this change fixes: before requester-binding, a digest
// read off one device's public acl record could be copied verbatim into a
// different device's own acl record and would validate identically, since
// nothing tied it to the requester at all. It must not validate for a
// device it wasn't computed for, even though every other input (granter,
// gateway, service) is identical.
func TestCheckACL_DigestCopiedToAnotherDeviceDenied(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)
	device1Pub := mustPub(t, device1Priv)
	device2Priv := strings.Repeat("66", 32)
	device2Pub := mustPub(t, device2Priv)

	digestForDevice1 := mustDigest(t, granterPriv, gatewayPub, device1Pub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		// device2 never received a grant — this is device1's digest, copied
		// verbatim into device2's own acl record.
		"device2": {ACL: []string{digestForDevice1}, Pubkey: strPtr(device2Pub)},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device2", "db")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a digest computed for device1 to be rejected when presented by device2")
	}
}

// TestCheckACL_RequesterWithNoPubkeyDenied: a device with no published
// pubkey couldn't have had a valid digest computed for it — treat it the
// same as "no acl" rather than erroring.
func TestCheckACL_RequesterWithNoPubkeyDenied(t *testing.T) {
	gatewayPriv := strings.Repeat("bb", 32)
	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, strings.Repeat("aa", 32)))},
		"device1":  {ACL: []string{"anything"}, Pubkey: nil},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a requester with no published pubkey to be denied")
	}
}

// TestCheckACL_WrongServiceNameDenied: a digest for "db" doesn't authorize
// "cache" — HMAC over a different message produces an unrelated digest.
func TestCheckACL_WrongServiceNameDenied(t *testing.T) {
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	gatewayPub := mustPub(t, gatewayPriv)
	device1Pub := mustPub(t, device1Priv)

	digest := mustDigest(t, granterPriv, gatewayPub, device1Pub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digest}, Pubkey: strPtr(device1Pub)},
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
	device1Pub := mustPub(t, device1Priv)

	digestForGateway1 := mustDigest(t, granterPriv, gateway1Pub, device1Pub, "db")

	resolver := fakeResolver{
		"gateway2": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digestForGateway1}, Pubkey: strPtr(device1Pub)},
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
		"device1":  {ACL: nil, Pubkey: strPtr(mustPub(t, device1Priv))},
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
	device1Pub := mustPub(t, device1Priv)

	digest := mustDigest(t, granterPriv, gatewayPub, device1Pub, "db")

	resolver := fakeResolver{
		"gateway1": {ACLGranters: []string{"ghost-granter", "granter1"}}, // ghost-granter doesn't resolve
		"granter1": {Pubkey: strPtr(mustPub(t, granterPriv))},
		"device1":  {ACL: []string{digest}, Pubkey: strPtr(device1Pub)},
	}

	ok, err := CheckACL(resolver, gatewayPriv, "gateway1", "device1", "db")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected an unresolvable granter to be skipped, not to block a later valid one")
	}
}
