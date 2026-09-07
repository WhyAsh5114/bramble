// Package admission is Gate 1.1's resolver+cache loop: it polls ENS state
// for a fixed set of peer labels and keeps a WireGuard device's peer table
// matched to it. This is the whole admission verifier — WireGuard's own
// protocol refuses a handshake from any key not in the peer table, so
// adding/removing peers correctly *is* enforcing admission (see
// docs/adr — no separate accept/reject hook exists or is needed).
//
// Authorization rule: pubkey is set AND expiry is in the future. The
// registry's `status` field is read but deliberately not used — its enum
// meaning isn't documented anywhere verified yet (see docs/12_SOURCE_NOTES.md).
// Revocation may turn out to need status once that's confirmed; using only
// expiry for now is a known, flagged gap, not a guess dressed up as one.
//
// Four revocation/change shapes exist and all four must actually mutate a
// live peer, not just refuse future handshakes (docs/05_BUILD_PLAN.md Gate
// 1.3): expiry lapsing (the record still reports a pubkey, just past
// expiry), a cleared pubkey text record (the record reports none at all),
// key rotation (the record reports a *different* pubkey than before, while
// remaining authorized — 02_TRACK_FIT.md's "a device can rotate its own
// key" pitch line), and a set `revoked` text record (Gate 2.1 — EAC-gated
// revocation via a role scoped to the `revoked` setter, distinct from the
// role scoped to the `pubkey` setter that rotation uses; see
// docs/adr/0004-admin-cli-writes-directly-to-registry.md). The first three
// need Loop to remember the last pubkey a label was authorized under — see
// knownPubkeys in SyncOnce — otherwise a peer that revokes by clearing its
// own record could never be found again to remove, and a rotated-away-from
// key would simply accumulate as a second, still-admitted peer entry
// forever (WireGuard's IpcSet keys peers by public key, so adding a new one
// never implicitly removes an old one). `revoked` doesn't have that problem
// — the pubkey is still reported, so the normal remove-by-pubkey path just
// works.
//
// Fail-open posture on resolver errors: a resolution error (sidecar down,
// RPC unreachable) does not by itself revoke a peer — see SyncOnce's error
// branch. This is deliberate: a transient RPC hiccup shouldn't tear down an
// otherwise-legitimate connection. Left unbounded, though, it also means a
// genuinely revoked peer stays admitted for as long as resolution keeps
// failing — an unstated assumption behind Gate 1.3's "bounded by TTL" claim,
// which implicitly assumes sidecar+RPC liveness. MaxStale bounds this: once
// a label has gone MaxStale past its last successful resolve, SyncOnce flips
// from fail-open to fail-closed and removes the peer. MaxStale's zero value
// preserves the original unbounded fail-open behavior; operators running a
// real demo or deployment should set it (a small multiple of TTL is
// reasonable) rather than rely on the default.
package admission

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

// Resolver is satisfied by *sidecar.Manager.
type Resolver interface {
	ResolveDevice(label string) (*sidecar.DeviceRecord, error)
}

// PeerTable is satisfied by *wgnode.Node.
type PeerTable interface {
	AddPeer(publicKeyHex string, allowedIP netip.Prefix, endpoint *netip.AddrPort) error
	RemovePeer(publicKeyHex string) error
}

// Peer is a mesh member this node should track. AllowedIP is supplied by the
// caller, not derived from ENS — a mesh IP addressing scheme is a record
// schema question Phase 2 owns (docs/05_BUILD_PLAN.md Phase 2 discretion),
// not decided here. Endpoint, if set, is used as-is (static config) and
// EndpointResolver is never consulted for this peer.
type Peer struct {
	Label     string
	AllowedIP netip.Prefix
	Endpoint  *netip.AddrPort
}

// Event reports one peer's admission decision after a sync pass, for logging.
type Event struct {
	Label       string
	Authorized  bool
	PublicKey   string // empty if never registered
	Err         error  // resolution or peer-table sync error, if any
	EndpointErr error  // EndpointResolver error, if any — non-fatal, peer is still added
}

type Loop struct {
	Resolver Resolver
	Table    PeerTable
	Peers    []Peer
	TTL      time.Duration

	// MaxStale bounds the fail-open window on resolver errors: once a label
	// has gone this long since its last successful resolve, SyncOnce removes
	// it from the peer table instead of leaving it admitted indefinitely.
	// Zero (the default) means unbounded fail-open — see the package
	// comment's "Fail-open posture" section before relying on the default.
	MaxStale time.Duration

	// EndpointResolver, if set, discovers a reachable endpoint for an
	// authorized peer that didn't already have a static Peer.Endpoint — e.g.
	// candidate exchange via the rendezvous relay (brambled/rendezvous). It's
	// consulted at most once per peer for the life of the Loop: once an
	// endpoint has been set, later sync passes don't re-resolve or re-set it,
	// both to avoid needless relay traffic and because WireGuard's own
	// roaming keeps the endpoint current once a real handshake happens. A
	// resolver error is logged via Event.EndpointErr but doesn't block
	// admission — the peer is still added without an endpoint (dial-in-first
	// still works, per wgnode.AddPeer's doc comment).
	EndpointResolver func(p Peer, publicKeyHex string) (*netip.AddrPort, error)

	// Now overrides the clock; tests set this. Defaults to time.Now.
	Now func() time.Time
	// OnEvent, if set, is called once per peer after every sync pass.
	OnEvent func(Event)

	resolvedEndpoints map[string]struct{}  // pubkey -> already resolved via EndpointResolver
	knownPubkeys      map[string]string    // label -> last pubkey this label was authorized under
	lastSuccess       map[string]time.Time // label -> time of last successful resolve
}

func (l *Loop) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// SyncOnce resolves every tracked peer and adds/removes it from the peer
// table accordingly. Idempotent — safe to call on every tick without diffing
// against prior state first.
func (l *Loop) SyncOnce() {
	for _, p := range l.Peers {
		record, err := l.Resolver.ResolveDevice(p.Label)
		if err != nil {
			l.emit(Event{Label: p.Label, Authorized: false, Err: err})
			l.handleResolveError(p)
			continue
		}
		l.markResolveSucceeded(p.Label)

		authorized := record.Pubkey != nil && *record.Pubkey != "" && l.expiryInFuture(record.Expiry) && !record.Revoked

		var pubkey string
		if record.Pubkey != nil {
			pubkey = *record.Pubkey
		}

		var syncErr, endpointErr error
		switch {
		case authorized:
			if old, rotated := l.knownPubkeys[p.Label]; rotated && old != pubkey {
				// Key rotation: the label is still authorized, but under a
				// different pubkey than last sync. WireGuard's IpcSet keys
				// peers by public key, so AddPeer below would add a second,
				// parallel entry rather than replacing this one — the old
				// key has to be explicitly removed first.
				_ = l.Table.RemovePeer(old)
				delete(l.resolvedEndpoints, old)
			}
			endpoint, epErr := l.resolveEndpoint(p, pubkey)
			endpointErr = epErr
			syncErr = l.Table.AddPeer(pubkey, p.AllowedIP, endpoint)
			if l.knownPubkeys == nil {
				l.knownPubkeys = make(map[string]string)
			}
			l.knownPubkeys[p.Label] = pubkey
		default:
			// The record's own pubkey covers "expired but not cleared" (it's
			// still reported, just past expiry). A *cleared* pubkey text
			// record reports none at all, so without remembering the last
			// pubkey this label was authorized under, a peer that revokes by
			// clearing its own record could never be found again to remove
			// from the live peer table — see docs/05_BUILD_PLAN.md Gate 1.3,
			// TestGate1_3_RevocationDropsAlreadyConnectedPeer.
			removeKey := pubkey
			if removeKey == "" {
				removeKey = l.knownPubkeys[p.Label]
			}
			if removeKey != "" {
				syncErr = l.Table.RemovePeer(removeKey)
				delete(l.resolvedEndpoints, removeKey)
				delete(l.knownPubkeys, p.Label)
			}
		}

		l.emit(Event{Label: p.Label, Authorized: authorized, PublicKey: pubkey, Err: syncErr, EndpointErr: endpointErr})
	}
}

// markResolveSucceeded records that label resolved successfully just now,
// resetting its staleness clock for handleResolveError.
func (l *Loop) markResolveSucceeded(label string) {
	if l.lastSuccess == nil {
		l.lastSuccess = make(map[string]time.Time)
	}
	l.lastSuccess[label] = l.now()
}

// handleResolveError enforces MaxStale: once a label has gone longer than
// MaxStale since its last successful resolve, the peer is removed from the
// live table (fail closed) instead of staying admitted indefinitely on a
// stuck resolver. A zero MaxStale disables this — see the package comment's
// "Fail-open posture" section.
func (l *Loop) handleResolveError(p Peer) {
	if l.MaxStale <= 0 {
		return
	}
	last, ok := l.lastSuccess[p.Label]
	if !ok || l.now().Sub(last) < l.MaxStale {
		return
	}
	removeKey, known := l.knownPubkeys[p.Label]
	if !known || removeKey == "" {
		return
	}
	syncErr := l.Table.RemovePeer(removeKey)
	delete(l.resolvedEndpoints, removeKey)
	delete(l.knownPubkeys, p.Label)
	delete(l.lastSuccess, p.Label)
	l.emit(Event{Label: p.Label, Authorized: false, Err: syncErr})
}

// resolveEndpoint returns p.Endpoint if set (static config wins outright),
// otherwise consults EndpointResolver at most once per pubkey for the life
// of the Loop. A resolver error is returned alongside a nil endpoint —
// callers add the peer anyway and surface the error via Event.EndpointErr.
func (l *Loop) resolveEndpoint(p Peer, pubkey string) (*netip.AddrPort, error) {
	if p.Endpoint != nil {
		return p.Endpoint, nil
	}
	if l.EndpointResolver == nil {
		return nil, nil
	}
	if _, done := l.resolvedEndpoints[pubkey]; done {
		return nil, nil
	}

	endpoint, err := l.EndpointResolver(p, pubkey)
	if err != nil {
		return nil, err
	}

	if l.resolvedEndpoints == nil {
		l.resolvedEndpoints = make(map[string]struct{})
	}
	l.resolvedEndpoints[pubkey] = struct{}{}
	return endpoint, nil
}

func (l *Loop) expiryInFuture(expiry string) bool {
	secs, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		return false
	}
	return time.Unix(secs, 0).After(l.now())
}

func (l *Loop) emit(e Event) {
	if l.OnEvent != nil {
		l.OnEvent(e)
	}
}

// Run syncs immediately, then on every TTL tick, until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) error {
	if l.TTL <= 0 {
		return fmt.Errorf("admission loop TTL must be positive")
	}

	l.SyncOnce()

	ticker := time.NewTicker(l.TTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			l.SyncOnce()
		}
	}
}
