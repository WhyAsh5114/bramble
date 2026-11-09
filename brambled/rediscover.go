package brambled

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
)

// defaultRediscoverInterval is how often serve re-lists the tailnet for
// devices enrolled after it started (docs/adr/0010, "Rediscovery").
const defaultRediscoverInterval = 15 * time.Second

// peerTracker is serve's live set of tracked peers, keyed by label and by
// mesh address. At startup it holds the peers resolvePeerMeshIPs returned.
// With auto-discovery on, rediscoverPeers adds to it while serve runs.
// The gateway reads it to map a tunnel source address to a label, so every
// method is safe for concurrent use.
type peerTracker struct {
	mu       sync.Mutex
	ownLabel string
	byLabel  map[string]admission.Peer
	byAddr   map[netip.Addr]string
	lastSkip map[string]string // label -> last skip reason logged, to log each reason once
}

func newPeerTracker(ownLabel string, self netip.Addr, peers []admission.Peer) *peerTracker {
	t := &peerTracker{
		ownLabel: ownLabel,
		byLabel:  make(map[string]admission.Peer, len(peers)),
		byAddr:   map[netip.Addr]string{self: ownLabel + " (self)"},
		lastSkip: map[string]string{},
	}
	for _, p := range peers {
		t.byLabel[p.Label] = p
		t.byAddr[p.AllowedIP.Addr()] = p.Label
	}
	return t
}

// lookup maps a tunnel source address to the tracked peer's label. It
// never returns this node's own address.
func (t *peerTracker) lookup(addr netip.Addr) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	label, ok := t.byAddr[addr]
	if !ok {
		return "", false
	}
	if _, peer := t.byLabel[label]; !peer {
		return "", false
	}
	return label, true
}

// discoverNew lists the tailnet's devices, resolves the mesh IP of each
// label not tracked yet, and adds the ones that resolve. It returns only
// the newly added peers. A label with no usable mesh-ip record yet, or with
// one that collides with a tracked address, is skipped and retried on the
// next call. The same skip reason is logged once, not every call.
//
// discoverNew only decides which labels serve tracks. Whether a tracked
// peer is admitted stays with admission.Loop (ENS pubkey, expiry, revoked),
// and what it may reach stays with the gateway ACL (docs/adr/0005).
func (t *peerTracker) discoverNew(lister deviceLister, resolver admission.Resolver, logw io.Writer) ([]admission.Peer, error) {
	candidates, err := discoverAllPeers(lister, t.ownLabel)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	var fresh []string
	for _, p := range candidates {
		if _, ok := t.byLabel[p.Label]; !ok {
			fresh = append(fresh, p.Label)
		}
	}
	t.mu.Unlock()

	// Resolve outside the lock: each call is a sidecar round trip, and the
	// gateway must not wait on it.
	type result struct {
		label  string
		prefix netip.Prefix
		err    error
	}
	results := make([]result, 0, len(fresh))
	for _, label := range fresh {
		prefix, err := resolveMeshIPPrefix(resolver, label)
		results = append(results, result{label, prefix, err})
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	var added []admission.Peer
	for _, r := range results {
		if _, ok := t.byLabel[r.label]; ok {
			continue
		}
		skip := ""
		var addr netip.Addr
		if r.err != nil {
			skip = r.err.Error()
		} else {
			addr = r.prefix.Addr()
			if other, taken := t.byAddr[addr]; taken {
				skip = fmt.Sprintf("mesh IP collision with %s", other)
			}
		}
		if skip != "" {
			if t.lastSkip[r.label] != skip {
				t.lastSkip[r.label] = skip
				fmt.Fprintf(logw, "serve: rediscovered peer %q skipped for now: %s\n", r.label, skip)
			}
			continue
		}
		delete(t.lastSkip, r.label)
		p := admission.Peer{Label: r.label, AllowedIP: netip.PrefixFrom(addr, 32)}
		t.byLabel[p.Label] = p
		t.byAddr[addr] = p.Label
		added = append(added, p)
	}
	return added, nil
}

// rediscoverPeers calls discoverNew every interval until ctx ends and hands
// each batch of new peers to onNew. A failed listing is logged and retried
// on the next tick. Peers are never removed here: admission.Loop already
// drops a peer whose record is revoked or expired.
func rediscoverPeers(ctx context.Context, interval time.Duration, t *peerTracker, lister deviceLister, resolver admission.Resolver, logw io.Writer, onNew func([]admission.Peer)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		added, err := t.discoverNew(lister, resolver, logw)
		if err != nil {
			fmt.Fprintf(logw, "serve: peer rediscovery failed, will retry: %v\n", err)
			continue
		}
		if len(added) > 0 {
			onNew(added)
		}
	}
}
