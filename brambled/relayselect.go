package brambled

import (
	"net"
	"sort"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
)

// The relay directory is open: anyone can list a relay. A node therefore never
// uses the whole directory. It takes the tailnet's own relays record first
// (written by the tailnet's admin, the same for every member), then the
// oldest directory entries, so two members of a tailnet always share a
// rendezvous relay and junk registrations cannot crowd out working ones.
const (
	maxRendezvousRelays = 4
	maxDataRelays       = 5
	// maxSTUNServers matches maxRendezvousRelays: chooseRelays takes at most
	// one STUN server from each rendezvous relay it picks.
	maxSTUNServers = maxRendezvousRelays
	// directoryFallback is how many of the oldest directory relays every node adds.
	directoryFallback = 3
)

// usableRelay reports whether a directory entry can be used at all.
func usableRelay(r registry.Relay) bool {
	if r.Pin == "" || r.Price != "0" {
		return false // no pin: cannot authenticate; a price: no payment path exists
	}
	host, port, err := net.SplitHostPort(r.Address)
	return err == nil && host != "" && port != ""
}

// chooseRelays picks the rendezvous set and the data-relay pool from the directory.
func chooseRelays(directory []registry.Relay, preferred []string) (rdv []rendezvous.Relay, data dataRelayFlag, stun []string) {
	byLabel := map[string]registry.Relay{}
	var usable []registry.Relay
	for _, r := range directory {
		if usableRelay(r) {
			byLabel[r.Label] = r
			usable = append(usable, r)
		}
	}
	sort.SliceStable(usable, func(i, j int) bool {
		if usable[i].Block != usable[j].Block {
			return usable[i].Block < usable[j].Block
		}
		return usable[i].Label < usable[j].Label
	})
	var picked []registry.Relay
	seen := map[string]bool{}
	add := func(r registry.Relay) {
		if !seen[r.Label] && len(picked) < maxRendezvousRelays {
			seen[r.Label] = true
			picked = append(picked, r)
		}
	}
	for _, label := range preferred {
		if r, ok := byLabel[label]; ok {
			add(r)
		}
	}
	// Oldest first, skipping relays this node has seen failing. A dead relay
	// fails for every member, so members converge on the same fallback set.
	fallback := 0
	for _, r := range usable {
		if fallback >= directoryFallback {
			break
		}
		if health.blocked(r.Label) || seen[r.Label] {
			continue
		}
		add(r)
		fallback++
	}
	data = dataRelayFlag{}
	for _, r := range picked {
		rr := rendezvous.Relay{Label: r.Label, Address: r.Address, Pin: r.Pin}
		rdv = append(rdv, rr)
		if r.DataRelay && len(data) < maxDataRelays {
			data[r.Label] = rr
		}
		if r.STUN != "" {
			stun = append(stun, r.STUN)
		}
	}
	return rdv, data, stun
}

// relayHealth remembers relays that keep failing, so a dead or hostile relay
// costs a timeout a few times, not on every connection.
type relayHealth struct {
	mu       sync.Mutex
	failures map[string]int
	until    map[string]time.Time
}

var health = &relayHealth{failures: map[string]int{}, until: map[string]time.Time{}}

const (
	healthStrikes  = 3
	healthCooldown = 10 * time.Minute
)

func (h *relayHealth) ok(label string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.failures, label)
	delete(h.until, label)
}

func (h *relayHealth) fail(label string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[label]++
	if h.failures[label] >= healthStrikes {
		h.until[label] = time.Now().Add(healthCooldown)
		h.failures[label] = 0
		tracef("relay %s failed %d times; skipping it for %s", label, healthStrikes, healthCooldown)
	}
}

func (h *relayHealth) blocked(label string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Now().Before(h.until[label])
}

// healthy drops blocked relays, unless that would leave none.
func healthy(relays []rendezvous.Relay) []rendezvous.Relay {
	var out []rendezvous.Relay
	for _, r := range relays {
		if !health.blocked(r.Label) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return relays
	}
	return out
}
