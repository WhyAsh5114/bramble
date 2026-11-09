package brambled

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
)

// relaySets is the relays a node uses: the rendezvous set, the data-relay
// pool and the STUN servers. A Fixed set came from a flag (-rendezvous,
// -data-relay, -stun-servers or -no-stun) and the directory never changes it.
type relaySets struct {
	Rendezvous      []rendezvous.Relay
	Data            dataRelayFlag
	STUN            []string
	FixedRendezvous bool
	FixedData       bool
	FixedSTUN       bool
}

// stunServer is a STUN server and the relay that lists it, so the server is
// dropped along with a relay that keeps failing.
type stunServer struct{ Relay, Addr string }

// relayFetch reads the directory once and returns what chooseRelays picked.
type relayFetch func() ([]rendezvous.Relay, dataRelayFlag, []stunServer, error)

// relayDirectory is the live view of the relays a node uses. serve reads it on
// every use instead of keeping a copy from startup, so a relay registered
// after the node started is used without a restart (docs/adr/0020).
//
// Each refresh updates the sets that are not Fixed. It first drops relays that
// relayHealth has blocked (3 unreachable attempts in a row), unless that would
// leave a set with nothing usable, then adds what chooseRelays picked up to
// the cap chooseRelays itself uses. A relay that is not blocked is never
// dropped: a live session may be using it.
type relayDirectory struct {
	mu      sync.Mutex
	fetch   relayFetch
	blocked func(label string) bool
	sets    relaySets
	stun    []stunServer // the STUN set with owners, when it is not Fixed
}

func newRelayDirectory(fetch relayFetch, initial relaySets) *relayDirectory {
	sets := initial
	sets.Rendezvous = append([]rendezvous.Relay(nil), initial.Rendezvous...)
	sets.Data = copyDataRelays(initial.Data)
	sets.STUN = append([]string(nil), initial.STUN...)
	return &relayDirectory{fetch: fetch, blocked: health.blocked, sets: sets}
}

// refresh asks the directory once and updates the sets. On an error the
// current sets stay as they are.
func (d *relayDirectory) refresh() error {
	rdv, data, stun, err := d.fetch()
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.sets.FixedRendezvous {
		label := func(r rendezvous.Relay) string { return r.Label }
		d.sets.Rendezvous = mergeRelays("rendezvous relay", d.sets.Rendezvous, rdv, label, label, d.blocked, maxRendezvousRelays)
	}
	if !d.sets.FixedData {
		label := func(r rendezvous.Relay) string { return r.Label }
		merged := mergeRelays("data relay", sortedRelays(d.sets.Data), sortedRelays(data), label, label, d.blocked, maxDataRelays)
		d.sets.Data = dataRelayFlag{}
		for _, r := range merged {
			d.sets.Data[r.Label] = r
		}
	}
	if !d.sets.FixedSTUN {
		addr := func(s stunServer) string { return s.Addr }
		owner := func(s stunServer) string { return s.Relay }
		d.stun = mergeRelays("STUN server", d.stun, stun, addr, owner, d.blocked, maxSTUNServers)
		d.sets.STUN = d.sets.STUN[:0]
		for _, s := range d.stun {
			d.sets.STUN = append(d.sets.STUN, s.Addr)
		}
	}
	return nil
}

// mergeRelays drops blocked entries from have, unless none of have or found
// would be left usable, then appends entries of found that are new and not
// blocked, up to max. id identifies an entry; owner is the relay label that
// blocked is asked about.
func mergeRelays[T any](kind string, have, found []T, id, owner func(T) string, blocked func(string) bool, max int) []T {
	usable := false
	for _, e := range append(append([]T(nil), have...), found...) {
		if !blocked(owner(e)) {
			usable = true
			break
		}
	}
	out := make([]T, 0, max)
	seen := map[string]bool{}
	for _, e := range have {
		if usable && blocked(owner(e)) {
			tracef("relays: %s %s dropped after repeated failures", kind, id(e))
			continue
		}
		seen[id(e)] = true
		out = append(out, e)
	}
	for _, e := range found {
		if seen[id(e)] || blocked(owner(e)) || len(out) >= max {
			continue
		}
		seen[id(e)] = true
		out = append(out, e)
		tracef("relays: %s %s added", kind, id(e))
	}
	return out
}

func sortedRelays(pool dataRelayFlag) []rendezvous.Relay {
	out := make([]rendezvous.Relay, 0, len(pool))
	for _, r := range pool {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// run refreshes every interval until ctx ends. A failed refresh is logged and
// retried on the next tick.
func (d *relayDirectory) run(ctx context.Context, interval time.Duration, logw io.Writer) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := d.refresh(); err != nil {
			fmt.Fprintf(logw, "relay discovery: %v (will retry)\n", err)
		}
	}
}

// Rendezvous returns the rendezvous relays known now, or an error when there
// are none yet. It is a rendezvousSource.
func (d *relayDirectory) Rendezvous() ([]rendezvous.Relay, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.sets.Rendezvous) == 0 {
		return nil, fmt.Errorf("no rendezvous relays known yet; relay discovery keeps retrying")
	}
	return append([]rendezvous.Relay(nil), d.sets.Rendezvous...), nil
}

// DataRelays returns a copy of the data-relay pool.
func (d *relayDirectory) DataRelays() dataRelayFlag {
	d.mu.Lock()
	defer d.mu.Unlock()
	return copyDataRelays(d.sets.Data)
}

// STUN returns a copy of the STUN server list.
func (d *relayDirectory) STUN() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.sets.STUN...)
}

func copyDataRelays(pool dataRelayFlag) dataRelayFlag {
	out := make(dataRelayFlag, len(pool))
	for label, r := range pool {
		out[label] = r
	}
	return out
}

// rendezvousSource returns the rendezvous relays known right now, or an error
// when there are none yet. It is a function, not a slice, so a set recovered
// after a startup discovery failure reaches every consumer.
type rendezvousSource func() ([]rendezvous.Relay, error)

// staticRendezvous is a rendezvousSource over a fixed set.
func staticRendezvous(relays []rendezvous.Relay) rendezvousSource {
	return func() ([]rendezvous.Relay, error) { return relays, nil }
}

// exchangeBackoff spaces repeated failed rendezvous exchanges with the same
// peer. A device that is enrolled but never runs a node (a stale test device)
// never answers, and every attempt buys a rendezvous token and blocks the
// admission loop for a full exchange timeout. After n consecutive failures
// the peer is skipped for min(base * 2^(n-1), max). A success clears it.
type exchangeBackoff struct {
	mu    sync.Mutex
	now   func() time.Time
	base  time.Duration
	max   time.Duration
	state map[string]backoffState
}

type backoffState struct {
	fails int
	next  time.Time
}

func newExchangeBackoff(base, max time.Duration) *exchangeBackoff {
	return &exchangeBackoff{now: time.Now, base: base, max: max, state: map[string]backoffState{}}
}

// Wait returns how long until key may be tried again; zero means now.
func (b *exchangeBackoff) Wait(key string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d := b.state[key].next.Sub(b.now()); d > 0 {
		return d
	}
	return 0
}

// Fail records a failed exchange with key.
func (b *exchangeBackoff) Fail(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state[key]
	s.fails++
	d := b.base
	for i := 1; i < s.fails && d < b.max; i++ {
		d *= 2
	}
	if d > b.max {
		d = b.max
	}
	s.next = b.now().Add(d)
	b.state[key] = s
}

// Succeed clears the failure history for key.
func (b *exchangeBackoff) Succeed(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, key)
}
