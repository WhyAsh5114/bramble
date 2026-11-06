package gateway

import (
	"fmt"
	"sync"

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
// The digest is bound to the requester's own pubkey (ACLDigestECDH's fourth
// argument): ACL records are public, so without this binding any device
// able to write its own acl record could copy a digest read off another
// device's public record and gain the same grant. Binding makes a digest
// only ever match the specific requester it was actually computed for.
//
// A granter or requester that fails to resolve is treated as "grants
// nothing" for that one identity rather than failing the whole check —
// consistent with admission's fail-open-per-label posture elsewhere in this
// codebase (see admission/loop.go's package comment), and because a
// transiently-unresolvable granter shouldn't itself deny an otherwise-valid
// request from a different, resolvable granter.
func CheckACL(resolver Resolver, myPrivateKeyHex, myLabel, requesterLabel, serviceName string) (bool, error) {
	fast := sidecar.FastResolveEnabled()
	var me, requester *sidecar.DeviceRecord
	var err, requesterErr error
	if fast {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			requester, requesterErr = resolver.ResolveDevice(requesterLabel)
		}()
		me, err = resolver.ResolveDevice(myLabel)
		wg.Wait()
	} else {
		me, err = resolver.ResolveDevice(myLabel)
	}
	if err != nil {
		return false, fmt.Errorf("resolving own record (%s): %w", myLabel, err)
	}

	if !fast {
		requester, requesterErr = resolver.ResolveDevice(requesterLabel)
	}
	if requesterErr != nil {
		return false, fmt.Errorf("resolving requester record (%s): %w", requesterLabel, requesterErr)
	}
	if len(requester.ACL) == 0 {
		return false, nil
	}
	if requester.Pubkey == nil || *requester.Pubkey == "" {
		// A digest is now bound to the requester's own pubkey (closes the
		// copy-a-digest-into-my-own-record gap — see docs/adr/0005). No
		// pubkey means no digest could ever have been legitimately computed
		// for this requester, so there's nothing to check.
		return false, nil
	}
	requesterACL := make(map[string]struct{}, len(requester.ACL))
	for _, digest := range requester.ACL {
		requesterACL[digest] = struct{}{}
	}

	// Authorization records are always fresh. Only their scheduling changes.
	type result struct {
		record *sidecar.DeviceRecord
		err    error
	}
	granters := make(chan result, len(me.ACLGranters))
	if fast {
		for _, label := range me.ACLGranters {
			go func() {
				record, err := resolver.ResolveDevice(label)
				// Buffered so a later result can finish after a valid grant
				// has already allowed the request.
				granters <- result{record: record, err: err}
			}()
		}
	}
	for _, granterLabel := range me.ACLGranters {
		var granter *sidecar.DeviceRecord
		var err error
		if fast {
			completed := <-granters
			granter, err = completed.record, completed.err
		} else {
			granter, err = resolver.ResolveDevice(granterLabel)
		}
		if err != nil || granter.Pubkey == nil || *granter.Pubkey == "" || granter.Revoked {
			continue
		}

		digest, err := ACLDigestECDH(myPrivateKeyHex, *granter.Pubkey, *requester.Pubkey, serviceName)
		if err != nil {
			continue
		}

		if _, ok := requesterACL[digest]; ok {
			return true, nil
		}
	}

	return false, nil
}
