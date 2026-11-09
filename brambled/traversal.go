package brambled

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/traversal"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

type traversalRun struct {
	key    string
	cancel context.CancelFunc
	done   chan struct{}
}

// peerTraversal starts only after admission has installed a peer. Network I/O
// stays outside the admission loop so rendezvous cannot delay revocation.
// Handle runs on the admission loop's goroutine.
type peerTraversal struct {
	ctx             context.Context
	node            *wgnode.Node
	own             string
	eligible        func(string) bool
	local           func() rendezvous.Announcement
	relaySet        rendezvousSource
	sessions        sessionSource
	pool            dataRelayFlag
	sessionBytes    int64
	exchangeTimeout time.Duration
	onState         func(string, string, string)
	allowLoopback   bool // rootless integration tests only
	runs            map[string]traversalRun
}

func (m *peerTraversal) Handle(e admission.Event) {
	if m.eligible != nil && !m.eligible(e.Label) {
		return
	}
	if e.Err != nil {
		return
	} // keep admission's bounded fail-open behavior
	if m.runs == nil {
		m.runs = make(map[string]traversalRun)
	}
	if previous, ok := m.runs[e.Label]; ok {
		running := true
		select {
		case <-previous.done:
			running = false
		default:
		}
		if e.Authorized && previous.key == e.PublicKey && running {
			return
		}
		previous.cancel()
		delete(m.runs, e.Label)
	}
	if !e.Authorized || e.PublicKey == "" {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	m.runs[e.Label] = traversalRun{e.PublicKey, cancel, done}
	go func() { defer close(done); defer cancel(); m.run(ctx, e.Label, e.PublicKey) }()
}

func (m *peerTraversal) admitted(key string) bool {
	peers, err := m.node.Peers()
	if err != nil {
		return false
	}
	for _, p := range peers {
		if p.PublicKeyHex == key {
			return true
		}
	}
	return false
}

func (m *peerTraversal) run(ctx context.Context, label, key string) {
	var mu sync.Mutex
	var relay netip.AddrPort
	var relayBind *rendezvous.RelayBind
	exchanger := rendezvous.Exchanger{
		Own: m.own, AllowLoopback: m.allowLoopback,
		Bind: func() *rendezvous.RelayBind {
			mu.Lock()
			defer mu.Unlock()
			return relayBind
		},
		OnBind: func(b rendezvous.RelayBind, endpoint netip.AddrPort) {
			relayBinder.keep(m.node, endpoint, b, '1', time.Now().Add(2*time.Hour))
		},
		Admitted: func(key string) bool { return ctx.Err() == nil && m.admitted(key) },
		Local: func() rendezvous.Announcement {
			local := append(rendezvous.Announcement(nil), m.local()...)
			mu.Lock()
			defer mu.Unlock()
			if relay.IsValid() {
				local = append(local, rendezvous.Endpoint{Kind: rendezvous.KindRelay, Addr: relay})
			}
			return local
		},
		Via: func(own, peer, candidate string) (string, error) {
			relays, err := m.relaySet()
			if err != nil {
				return "", err
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			learned, _, err := exchangeAny(relays, own, peer, candidate, m.exchangeTimeout)
			return learned, err
		},
	}
	controller := traversal.Controller{
		Node: m.node, Peer: key,
		Learn: func(ctx context.Context) (rendezvous.Announcement, error) {
			return exchanger.Learn(key)
		},
		Relay: func(ctx context.Context) (netip.AddrPort, error) {
			// Both ends must use one port-per-session allocation, never two.
			// The lower public key buys; the other learns its relay announcement.
			if m.own > key {
				return netip.AddrPort{}, fmt.Errorf("waiting for peer's relay session")
			}
			if ctx.Err() != nil || !m.admitted(key) {
				return netip.AddrPort{}, rendezvous.ErrNotAdmitted
			}
			session, err := buyDataRelaySession(m.sessions, m.pool, m.sessionBytes)
			if err != nil {
				return netip.AddrPort{}, err
			}
			endpoint, err := netip.ParseAddrPort(session.candidate)
			if err != nil {
				return netip.AddrPort{}, err
			}
			if ctx.Err() != nil || !m.admitted(key) {
				return netip.AddrPort{}, rendezvous.ErrNotAdmitted
			}
			relayBinder.keep(m.node, endpoint, session.bind, '0', session.expiresAt)
			mu.Lock()
			relay, relayBind = endpoint, &session.bind
			mu.Unlock()
			return endpoint, nil
		},
		OnState: func(state string) {
			if m.onState != nil && ctx.Err() == nil {
				m.onState(label, key, state)
			}
		},
	}
	controller.Run(ctx)
}
