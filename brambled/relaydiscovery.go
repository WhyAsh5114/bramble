package brambled

import (
	"context"
	"fmt"
	"io"
	"slices"
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

// relayDirectory is the live view of the relays a node uses. serve reads it on
// every use instead of keeping a copy from start-up, so a relay registered
// after the node started is used without a restart (docs/adr/0020).
//
// Each refresh merges what chooseRelays picked into the sets that are not
// Fixed. Relays are added, never removed: a live session may be using one, and
// relayHealth already steers new work away from a failing relay. Each set is
// capped at the size chooseRelays itself returns.
type relayDirectory struct {
	mu    sync.Mutex
	fetch func() ([]rendezvous.Relay, dataRelayFlag, []string, error)
	sets  relaySets
}

func newRelayDirectory(fetch func() ([]rendezvous.Relay, dataRelayFlag, []string, error), initial relaySets) *relayDirectory {
	sets := initial
	sets.Rendezvous = append([]rendezvous.Relay(nil), initial.Rendezvous...)
	sets.Data = copyDataRelays(initial.Data)
	sets.STUN = append([]string(nil), initial.STUN...)
	return &relayDirectory{fetch: fetch, sets: sets}
}

// refresh asks the directory once and merges the result. On an error the
// current sets stay as they are.
func (d *relayDirectory) refresh() error {
	rdv, data, stun, err := d.fetch()
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.sets.FixedRendezvous {
		have := map[string]bool{}
		for _, r := range d.sets.Rendezvous {
			have[r.Label] = true
		}
		for _, r := range rdv {
			if !have[r.Label] && len(d.sets.Rendezvous) < maxRendezvousRelays {
				have[r.Label] = true
				d.sets.Rendezvous = append(d.sets.Rendezvous, r)
				tracef("relays: rendezvous relay %s added", r.Label)
			}
		}
	}
	if !d.sets.FixedData {
		labels := make([]string, 0, len(data))
		for label := range data {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			if _, ok := d.sets.Data[label]; !ok && len(d.sets.Data) < maxDataRelays {
				d.sets.Data[label] = data[label]
				tracef("relays: data relay %s added", label)
			}
		}
	}
	if !d.sets.FixedSTUN {
		for _, server := range stun {
			if !slices.Contains(d.sets.STUN, server) && len(d.sets.STUN) < maxRendezvousRelays {
				d.sets.STUN = append(d.sets.STUN, server)
				tracef("relays: STUN server %s added", server)
			}
		}
	}
	return nil
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
		return nil, fmt.Errorf("no rendezvous relays known yet; relay discovery retries every %s", relayRefreshInterval)
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
// after a start-up discovery failure reaches every consumer.
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
