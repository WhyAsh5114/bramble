package brambled

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
)

// rendezvousPool is the set of rendezvous relays the endpoint resolver
// uses. It starts from what start-up discovery found. If start-up discovery
// failed (the RPC pool rejected the relay registry scan, found live on the
// cloud nodes on 6 Nov 2026), the pool asks again on a later call instead of
// staying empty for the life of the process. Without this, one failed scan
// left the worker with no resolver at all: it never posted its candidate, so
// no gateway could reach it.
type rendezvousPool struct {
	mu         sync.Mutex
	relays     []rendezvous.Relay
	fetch      func() ([]rendezvous.Relay, error)
	now        func() time.Time
	retryAfter time.Duration
	lastTry    time.Time
}

// rendezvousSource returns the rendezvous relays known right now, or an error
// when there are none yet. It is a function, not a slice, so a set recovered
// after a start-up discovery failure reaches every consumer.
type rendezvousSource func() ([]rendezvous.Relay, error)

// staticRendezvous is a rendezvousSource over a fixed set.
func staticRendezvous(relays []rendezvous.Relay) rendezvousSource {
	return func() ([]rendezvous.Relay, error) { return relays, nil }
}

func newRendezvousPool(initial []rendezvous.Relay, fetch func() ([]rendezvous.Relay, error), retryAfter time.Duration) *rendezvousPool {
	return &rendezvousPool{relays: initial, fetch: fetch, now: time.Now, retryAfter: retryAfter}
}

// Get returns the known relays. When there are none, it queries discovery
// again, at most once per retryAfter. The caller treats an error as "retry on
// the next sync".
func (p *rendezvousPool) Get() ([]rendezvous.Relay, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.relays) > 0 {
		return p.relays, nil
	}
	if !p.lastTry.IsZero() && p.now().Sub(p.lastTry) < p.retryAfter {
		return nil, fmt.Errorf("no rendezvous relays known; discovery was retried less than %s ago", p.retryAfter)
	}
	p.lastTry = p.now()
	resp, err := p.fetch()
	if err != nil {
		return nil, fmt.Errorf("relay discovery retry: %w", err)
	}
	p.relays = append(p.relays, resp...)
	if len(p.relays) == 0 {
		return nil, fmt.Errorf("relay discovery retry found no rendezvous relays")
	}
	fmt.Fprintf(os.Stderr, "relay discovery: retry found %d rendezvous relay(s)\n", len(p.relays))
	return p.relays, nil
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
