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
// not decided here.
type Peer struct {
	Label     string
	AllowedIP netip.Prefix
}

// Event reports one peer's admission decision after a sync pass, for logging.
type Event struct {
	Label      string
	Authorized bool
	PublicKey  string // empty if never registered
	Err        error  // resolution error, if any — treated as not-authorized
}

type Loop struct {
	Resolver Resolver
	Table    PeerTable
	Peers    []Peer
	TTL      time.Duration

	// Now overrides the clock; tests set this. Defaults to time.Now.
	Now func() time.Time
	// OnEvent, if set, is called once per peer after every sync pass.
	OnEvent func(Event)
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
			continue
		}

		authorized := record.Pubkey != nil && *record.Pubkey != "" && l.expiryInFuture(record.Expiry)

		var pubkey string
		if record.Pubkey != nil {
			pubkey = *record.Pubkey
		}

		var syncErr error
		switch {
		case authorized:
			syncErr = l.Table.AddPeer(pubkey, p.AllowedIP, nil)
		case pubkey != "":
			// Only known keys can be meaningfully removed; a peer that was
			// never registered was never added in the first place.
			syncErr = l.Table.RemovePeer(pubkey)
		}

		l.emit(Event{Label: p.Label, Authorized: authorized, PublicKey: pubkey, Err: syncErr})
	}
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
