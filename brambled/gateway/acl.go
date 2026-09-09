package gateway

import (
	"fmt"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

// Resolver is satisfied by *sidecar.Manager — same shape as
// admission.Resolver (brambled/admission/loop.go), declared separately here
// rather than imported from admission so gateway doesn't depend on the
// admission package for an unrelated reason (Go interface satisfaction
// requires the return type to be the literal sidecar.DeviceRecord, not a
// structurally-equivalent local type, so both packages import sidecar
// directly instead).
type Resolver interface {
	ResolveDevice(label string) (*sidecar.DeviceRecord, error)
}

// CheckACL implements adr/0005's verification loop: a gateway (identified by
// myLabel/myPrivateKeyHex) decides whether requesterLabel may reach
// serviceName by iterating its own trusted acl-granters, computing the
// candidate digest each one would have produced, and checking membership in
// the requester's own acl record. No value is ever transmitted for this
// check — every input is either already-public chain state or this
// gateway's own private key.
//
// A granter or requester that fails to resolve is treated as "grants
// nothing" for that one identity rather than failing the whole check —
// consistent with admission's fail-open-per-label posture elsewhere in this
// codebase (see admission/loop.go's package comment), and because a
// transiently-unresolvable granter shouldn't itself deny an otherwise-valid
// request from a different, resolvable granter.
func CheckACL(resolver Resolver, myPrivateKeyHex, myLabel, requesterLabel, serviceName string) (bool, error) {
	me, err := resolver.ResolveDevice(myLabel)
	if err != nil {
		return false, fmt.Errorf("resolving own record (%s): %w", myLabel, err)
	}

	requester, err := resolver.ResolveDevice(requesterLabel)
	if err != nil {
		return false, fmt.Errorf("resolving requester record (%s): %w", requesterLabel, err)
	}
	if len(requester.ACL) == 0 {
		return false, nil
	}
	requesterACL := make(map[string]struct{}, len(requester.ACL))
	for _, digest := range requester.ACL {
		requesterACL[digest] = struct{}{}
	}

	for _, granterLabel := range me.ACLGranters {
		granter, err := resolver.ResolveDevice(granterLabel)
		if err != nil || granter.Pubkey == nil || *granter.Pubkey == "" {
			continue
		}

		digest, err := ACLDigestECDH(myPrivateKeyHex, *granter.Pubkey, serviceName)
		if err != nil {
			continue
		}

		if _, ok := requesterACL[digest]; ok {
			return true, nil
		}
	}

	return false, nil
}
