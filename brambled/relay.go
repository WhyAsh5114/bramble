package brambled

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/datarelay"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

// sessionSource gets data-relay sessions. The production source asks the
// relay itself over its pinned TLS channel; tests substitute their own.
type sessionSource interface {
	Session(relay rendezvous.Relay, bytes int64) (*rendezvous.SessionGrant, error)
}

// trace logs path decisions when BRAMBLE_TRACE is set. It never logs keys,
// session secrets or candidates' bind extensions.
var traceOn = os.Getenv("BRAMBLE_TRACE") != ""

func tracef(format string, args ...any) {
	if traceOn {
		fmt.Fprintf(os.Stderr, "trace: "+format+"\n", args...)
	}
}

type freeSessions struct{}

const (
	dataRelayRequestTimeout = 8 * time.Second
	dataRelayProbeTimeout   = 3 * time.Second
)

func (freeSessions) Session(r rendezvous.Relay, bytes int64) (*rendezvous.SessionGrant, error) {
	return rendezvous.RequestSession(r, bytes, dataRelayRequestTimeout)
}

// dataRelayFlag is the pool of relays that offer data sessions, by label.
// The -data-relay flag takes label=host:port@pin; discovery fills it from the
// relay directory otherwise.
type dataRelayFlag map[string]rendezvous.Relay

func (d dataRelayFlag) String() string { return fmt.Sprintf("%v", map[string]rendezvous.Relay(d)) }

func (d dataRelayFlag) Set(value string) error {
	label, rest, ok := strings.Cut(value, "=")
	addr, pin, ok2 := strings.Cut(rest, "@")
	if !ok || !ok2 || label == "" || addr == "" || pin == "" {
		return fmt.Errorf("expected label=host:port@pin, got %q", value)
	}
	d[label] = rendezvous.Relay{Label: label, Address: addr, Pin: pin}
	return nil
}

// relayPeerFlag accumulates repeated -relay-peer label flags: tracked peers
// routed through the data-relay pool instead of a direct candidate.
type relayPeerFlag map[string]bool

func (r relayPeerFlag) String() string { return fmt.Sprintf("%v", map[string]bool(r)) }

func (r relayPeerFlag) Set(value string) error {
	if value == "" {
		return fmt.Errorf("expected a tracked -peer label, got empty string")
	}
	r[value] = true
	return nil
}

// dataRelaySession is one allocation this node holds and shares with a peer.
type dataRelaySession struct {
	id           string
	relayLabel   string
	relay        rendezvous.Relay
	candidate    string // ip:port of the session's UDP socket
	bind         rendezvous.RelayBind
	grantedBytes int64
	expiresAt    time.Time
}

// resolveEndpoint turns a relay's host:port into ip:port; a relay may publish a DNS name.
func resolveEndpoint(hostport string) (netip.AddrPort, error) {
	if ap, err := netip.ParseAddrPort(hostport); err == nil {
		return ap, nil
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return netip.AddrPort{}, err
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return netip.AddrPort{}, fmt.Errorf("resolving %s: %v", host, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			if a, ok := netip.AddrFromSlice(v4); ok {
				return netip.ParseAddrPort(net.JoinHostPort(a.String(), port))
			}
		}
	}
	a, _ := netip.AddrFromSlice(ips[0])
	return netip.ParseAddrPort(net.JoinHostPort(a.String(), port))
}

// buyDataRelaySession asks every relay in pool for its status, skips relays
// whose budget is used up, and takes a session from the one that answered
// fastest. Nothing is paid; the name is kept for the failover code's sake.
func buyDataRelaySession(m sessionSource, pool dataRelayFlag, bytes int64) (*dataRelaySession, error) {
	if bytes <= 0 {
		return nil, fmt.Errorf("data-relay session bytes must be positive")
	}
	// Probe in parallel with a short timeout: a slow or dead relay must not
	// hold up the others.
	type probe struct {
		quote datarelay.Quote
		err   error
	}
	probes := make(chan probe, len(pool))
	for label, r := range pool {
		go func(label string, r rendezvous.Relay) {
			if health.blocked(label) {
				probes <- probe{err: fmt.Errorf("%s: skipped after repeated failures", label)}
				return
			}
			start := time.Now()
			st, err := rendezvous.Status(r, dataRelayProbeTimeout)
			switch {
			case err != nil:
				health.fail(label)
				probes <- probe{err: fmt.Errorf("%s: %w", label, err)}
			case st.Budget.ForwardingPaused:
				probes <- probe{err: fmt.Errorf("%s: its data budget is used up for now", label)}
			default:
				health.ok(label)
				probes <- probe{quote: datarelay.Quote{Label: label, LatencyMS: time.Since(start).Milliseconds()}}
			}
		}(label, r)
	}
	quotes := make([]datarelay.Quote, 0, len(pool))
	for range pool {
		p := <-probes
		if p.err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: skipping %v\n", p.err)
			continue
		}
		quotes = append(quotes, p.quote)
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("no data relay is reachable with budget left")
	}
	winner := pool[datarelay.Pick(quotes).Label]
	tracef("data-relay: candidates %v, picked %s", quotes, winner.Label)
	grant, err := m.Session(winner, bytes)
	if err != nil {
		return nil, fmt.Errorf("session from %s: %w", winner.Label, err)
	}
	endpoint, err := resolveEndpoint(grant.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("session from %s: %w", winner.Label, err)
	}
	id, err1 := hex.DecodeString(grant.ID)
	secret, err2 := hex.DecodeString(grant.Secret)
	if err1 != nil || err2 != nil || len(id) != 8 || len(secret) < 16 {
		return nil, fmt.Errorf("session from %s: malformed grant", winner.Label)
	}
	s := &dataRelaySession{
		id: grant.ID, relayLabel: winner.Label, relay: winner, candidate: endpoint.String(),
		bind: rendezvous.RelayBind{Secret: secret}, grantedBytes: grant.Bytes, expiresAt: time.Unix(grant.ExpiresAt, 0),
	}
	copy(s.bind.ID[:], id)
	return s, nil
}

// dataRelayKeepaliveSeconds keeps a relay-routed peer's path warm. It must be
// well under the relay's session lifetime.
const dataRelayKeepaliveSeconds = 5

// relayNode is what the relay paths need from *wgnode.Node.
type relayNode interface {
	SetPersistentKeepalive(publicKeyHex string, seconds int) error
	SendRaw(target netip.AddrPort, payload []byte) error
	Peers() ([]wgnode.PeerInfo, error)
}

// armWhenInstalled sets the keepalive once admission has installed the peer
// with the relay endpoint. Setting it earlier makes WireGuard attempt a
// handshake with no endpoint, and the first real attempt then waits for the
// 5 s retry timer (seen with BRAMBLE_TRACE: every relayed connect took 5 s).
func armWhenInstalled(node relayNode, peerPubkey string, endpoint netip.AddrPort) {
	go func() {
		deadline := time.Now().Add(time.Minute)
		for time.Now().Before(deadline) {
			if peers, err := node.Peers(); err == nil {
				for _, p := range peers {
					if p.PublicKeyHex == peerPubkey && p.Endpoint == endpoint.String() {
						err := node.SetPersistentKeepalive(peerPubkey, dataRelayKeepaliveSeconds)
						tracef("data-relay: keepalive armed for %s (err=%v)", short(peerPubkey), err)
						return
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

// binder keeps sending bind packets for active sessions, so the relay keeps
// forwarding after a NAT remaps a port. One loop runs per session and side.
type binder struct {
	mu     sync.Mutex
	active map[string]time.Time // session id + side -> stop time
}

var relayBinder = &binder{active: map[string]time.Time{}}

const bindInterval = 10 * time.Second

// keep binds node's WireGuard socket to side of the session at endpoint until `until`.
func (b *binder) keep(node relayNode, endpoint netip.AddrPort, rb rendezvous.RelayBind, side byte, until time.Time) {
	key := hex.EncodeToString(rb.ID[:]) + string(side)
	b.mu.Lock()
	if stop, ok := b.active[key]; ok && !until.After(stop) {
		b.mu.Unlock()
		return
	}
	_, running := b.active[key]
	b.active[key] = until
	b.mu.Unlock()
	send := func() {
		err := node.SendRaw(endpoint, relaysrv.MakeBind(rb.Secret, side, rb.ID, time.Now()))
		tracef("bind: session %x side %c -> %s (err=%v)", rb.ID, side, endpoint, err)
	}
	send()
	if running {
		return
	}
	go func() {
		t := time.NewTicker(bindInterval)
		defer t.Stop()
		for range t.C {
			b.mu.Lock()
			stop := b.active[key]
			if time.Now().After(stop) {
				delete(b.active, key)
				b.mu.Unlock()
				return
			}
			b.mu.Unlock()
			send()
		}
	}()
}

// relayRoutedEndpoint routes a -relay-peer through a data-relay session: it
// takes a session, binds this node to side 0, and shares the session with the
// peer through the sealed rendezvous channel. Both sides point WireGuard at the
// relay's session socket.
func relayRoutedEndpoint(node relayNode, m sessionSource, pool dataRelayFlag, sessionBytes int64, relays []rendezvous.Relay, own, peerPubkey string, ttl time.Duration, session *dataRelaySession) (*netip.AddrPort, *dataRelaySession, error) {
	if session == nil || !time.Now().Before(session.expiresAt) {
		var err error
		session, err = buyDataRelaySession(m, pool, sessionBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("getting a data-relay session: %w", err)
		}
	}
	relayEndpoint, err := netip.ParseAddrPort(session.candidate)
	if err != nil {
		return nil, session, fmt.Errorf("data-relay candidate %q unparseable: %w", session.candidate, err)
	}
	relayBinder.keep(node, relayEndpoint, session.bind, '0', session.expiresAt)
	if err := publishDataRelaySession(session, relays, own, peerPubkey, ttl); err != nil {
		return nil, session, err
	}
	armWhenInstalled(node, peerPubkey, relayEndpoint)
	return &relayEndpoint, session, nil
}

// publishDataRelaySession offers the session to the peer. The bind secret
// travels only inside the sealed candidate.
func publishDataRelaySession(session *dataRelaySession, relays []rendezvous.Relay, own, peerPubkey string, ttl time.Duration) error {
	if !time.Now().Before(session.expiresAt) {
		return fmt.Errorf("data-relay session expired before publication")
	}
	fmt.Fprintf(os.Stderr, "data-relay: sharing session %s from %s (%d bytes), routing %s via %s\n",
		session.id, session.relayLabel, session.grantedBytes, short(peerPubkey), session.candidate)
	candidate := rendezvous.WithBind("ep1:relay="+session.candidate+",static="+session.candidate, session.bind)
	_, _, err := exchangeAny(relays, own, peerPubkey, candidate, ttl)
	return err
}

// adoptBind binds this node to side 1 of a session a peer shared in
// candidate, if any. The bind packets also keep the NAT mapping and the
// relay's slot alive, so this side does not arm a WireGuard keepalive: only
// the session holder initiates handshakes. With both sides keeping alive on
// the same schedule, their initiations collide and neither handshake
// completes (seen in TestRelayRoutedPeersTalkThroughABoundSession).
func adoptBind(node relayNode, candidate string, endpoint netip.AddrPort) bool {
	if _, rb := rendezvous.SplitBind(candidate); rb != nil {
		relayBinder.keep(node, endpoint, *rb, '1', time.Now().Add(2*time.Hour))
		return true
	}
	return false
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// Failover tuning (docs/adr/0008): how often to sample RxBytes and how long
// it must sit still before a relay counts as stalled.
const (
	dataRelayPollInterval = 2 * time.Second
	dataRelayStallTimeout = 6 * time.Second
)

// relayState records which data relay each -relay-peer currently uses,
// keyed by label so a key rotation carries over.
type relayState struct {
	mu       sync.Mutex
	pubkey   map[string]string
	sessions map[string]*dataRelaySession
}

func newRelayState() *relayState {
	return &relayState{pubkey: make(map[string]string), sessions: make(map[string]*dataRelaySession)}
}

func (s *relayState) set(peerLabel, pubkey string, session *dataRelaySession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pubkey[peerLabel] = pubkey
	s.sessions[peerLabel] = session
}

func (s *relayState) get(peerLabel string) (pubkey, relayLabel string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pubkey, hasPubkey := s.pubkey[peerLabel]
	session, hasRelay := s.sessions[peerLabel]
	if !hasPubkey || !hasRelay {
		return pubkey, "", false
	}
	return pubkey, session.relayLabel, true
}

func (s *relayState) session(peerLabel, pubkey string) *dataRelaySession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pubkey[peerLabel] != pubkey {
		return nil
	}
	return s.sessions[peerLabel]
}

func (s *relayState) replace(peerLabel, pubkey string, previous, next *dataRelaySession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pubkey[peerLabel] != pubkey || s.sessions[peerLabel] != previous {
		return false
	}
	s.sessions[peerLabel] = next
	return true
}

func (s *relayState) setPubkey(peerLabel, pubkey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pubkey[peerLabel] = pubkey
}

func (s *relayState) getPubkey(peerLabel string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pubkey, ok := s.pubkey[peerLabel]
	return pubkey, ok
}

// staticRendezvousRelays is the rendezvous set for an explicit -rendezvous host:port@pin.
func staticRendezvousRelays(value string) []rendezvous.Relay {
	if value == "" {
		return []rendezvous.Relay{}
	}
	addr, pin, _ := strings.Cut(value, "@")
	return []rendezvous.Relay{{Label: "static", Address: addr, Pin: pin}}
}

// exchangeAny tries every rendezvous relay at once (ADR 0003) and returns the
// first successful exchange, so one rogue or unreachable relay cannot block a
// connection. Each relay gets the full timeout.
func exchangeAny(relays []rendezvous.Relay, ownPubkey, peerPubkey, myCandidate string, exchangeTimeout time.Duration) (candidate, usedLabel string, err error) {
	if len(relays) == 0 {
		return "", "", fmt.Errorf("no rendezvous relays to try")
	}
	type attemptResult struct {
		candidate, label string
		err              error
	}
	relays = healthy(relays)
	results := make(chan attemptResult, len(relays))
	for _, r := range relays {
		go func(r rendezvous.Relay) {
			c, err := rendezvous.Exchange(r, ownPubkey, peerPubkey, myCandidate, exchangeTimeout)
			switch {
			case err == nil:
				health.ok(r.Label)
			case errors.Is(err, rendezvous.ErrRelayUnreachable):
				health.fail(r.Label)
			}
			results <- attemptResult{candidate: c, label: r.Label, err: err}
		}(r)
	}
	var lastErr error
	for range relays {
		res := <-results
		if res.err == nil {
			base, b := rendezvous.SplitBind(res.candidate)
			tracef("rendezvous: %s answered for %s: %q (shares session: %v)", res.label, short(peerPubkey), base, b != nil)
			return res.candidate, res.label, nil
		}
		fmt.Fprintf(os.Stderr, "rendezvous: %s: exchange failed: %v\n", res.label, res.err)
		lastErr = res.err
	}
	return "", "", lastErr
}

func relayPeerSnapshot(node *wgnode.Node, pubkey string) (wgnode.PeerInfo, bool) {
	peers, err := node.Peers()
	if err == nil {
		for _, peer := range peers {
			if peer.PublicKeyHex == pubkey {
				return peer, true
			}
		}
	}
	return wgnode.PeerInfo{}, false
}

// watchDataRelayFailover keeps a -relay-peer connected: once its session
// stalls (a dead relay, a used-up session or budget, or an expired TTL) it
// takes a session from another relay, re-points WireGuard and shares the new
// session. Before traffic confirms a session, it re-shares the same one until
// expiry. With every relay stalled, it starts a new round over the whole pool.
// Failed attempts back off (failoverBackoff).
func watchDataRelayFailover(ctx context.Context, node *wgnode.Node, m sessionSource, state *relayState, peerLabel string, pool func() dataRelayFlag, sessionBytes int64, relaySet rendezvousSource, ownPubkey string, ttl time.Duration) {
	excluded := map[string]bool{}
	failures := 0
	var observed *dataRelaySession
	var baselineRx int64
	confirmed := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		pubkey, usedLabel, ok := state.get(peerLabel)
		if !ok {
			continue
		}
		session := state.session(peerLabel, pubkey)
		if session == nil {
			continue
		}
		if session != observed {
			observed, baselineRx, confirmed = session, 0, false
		}
		if err := datarelay.WaitForStall(ctx, node, pubkey, dataRelayPollInterval, dataRelayStallTimeout); err != nil {
			return
		}
		peer, found := relayPeerSnapshot(node, pubkey)
		if !found || state.session(peerLabel, pubkey) != session {
			continue
		}
		if !confirmed && peer.RxBytes > baselineRx && peer.Endpoint == session.candidate {
			confirmed = true
			failures = 0
		}
		reuse := !confirmed && time.Now().Before(session.expiresAt)
		remaining := dataRelayFlag{}
		if !reuse {
			fmt.Fprintf(os.Stderr, "data-relay: %s via %s stalled — failing over\n", peerLabel, usedLabel)
			excluded[usedLabel] = true
			all := pool()
			for label, r := range all {
				if !excluded[label] {
					remaining[label] = r
				}
			}
			if len(remaining) == 0 {
				excluded = map[string]bool{}
				for label, r := range all {
					remaining[label] = r
				}
			}
		}
		if wait := failoverBackoff(failures); wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		relays, err := relaySet()
		if err != nil {
			continue
		}
		current, found := relayPeerSnapshot(node, pubkey)
		if !found || state.session(peerLabel, pubkey) != session || current.RxBytes != peer.RxBytes {
			continue
		}
		if !reuse {
			next, err := buyDataRelaySession(m, remaining, sessionBytes)
			if err != nil {
				failures++
				fmt.Fprintf(os.Stderr, "data-relay: %s: allocation failed, will retry: %v\n", peerLabel, err)
				continue
			}
			current, found = relayPeerSnapshot(node, pubkey)
			if !found || state.session(peerLabel, pubkey) != session || current.RxBytes != peer.RxBytes {
				continue
			}
			endpoint, err := netip.ParseAddrPort(next.candidate)
			if err != nil {
				failures++
				continue
			}
			relayBinder.keep(node, endpoint, next.bind, '0', next.expiresAt)
			// update_only keeps this from resurrecting a key admission removed meanwhile.
			if err := node.UpdatePeerEndpoint(pubkey, endpoint, dataRelayKeepaliveSeconds); err != nil {
				failures++
				continue
			}
			current, found = relayPeerSnapshot(node, pubkey)
			if !found || !state.replace(peerLabel, pubkey, session, next) {
				continue
			}
			session = next
			observed, baselineRx, confirmed = next, current.RxBytes, false
		}
		if err := publishDataRelaySession(session, relays, ownPubkey, pubkey, ttl); err != nil {
			failures++
			fmt.Fprintf(os.Stderr, "data-relay: %s: publication failed, will retry the allocation until expiry: %v\n", peerLabel, err)
			continue
		}
		failures++
	}
}

// failoverBackoff is the extra wait after consecutive attempts without
// received traffic: none for the first retry, then doubling from 5 s to 3 min.
func failoverBackoff(failures int) time.Duration {
	if failures < 2 {
		return 0
	}
	wait := 5 * time.Second
	for i := 2; i < failures && wait < failoverMaxBackoff; i++ {
		wait *= 2
	}
	return min(wait, failoverMaxBackoff)
}

const failoverMaxBackoff = 3 * time.Minute

// dataRelayAdoptExchangeTimeout is the per-cycle exchange budget of the adopt
// watcher. Short on purpose: an exchange returns the first offer that arrives,
// so a long call could adopt a stale one.
const dataRelayAdoptExchangeTimeout = 3 * time.Second

const (
	// relayRefreshInterval is how often serve re-reads the relay directory.
	// The directory is read from the local registry cache, so a pass is cheap.
	relayRefreshInterval = 15 * time.Second
	exchangeBackoffMax   = 2 * time.Minute
)

// The adopt watcher polls only while the peer's traffic has stalled (the same
// RxBytes signal the failover side acts on), so an idle healthy link costs
// nothing at the relay.

// watchDataRelayAdopt is the receiving side of failover: when the peer that
// holds the session moves to another relay, this node learns the new session
// over rendezvous, binds to it and re-points WireGuard.
func watchDataRelayAdopt(ctx context.Context, node *wgnode.Node, peerLabel, ownPubkey, peerPubkey, ownCandidate string, allowedIP netip.Prefix, relaySet rendezvousSource) {
	var current string
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := datarelay.WaitForStall(ctx, node, peerPubkey, dataRelayPollInterval, dataRelayStallTimeout); err != nil {
			return
		}
		started := time.Now()
		var candidate string
		relays, err := relaySet()
		if err == nil {
			candidate, _, err = exchangeAny(relays, ownPubkey, peerPubkey, ownCandidate, dataRelayAdoptExchangeTimeout)
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(dataRelayAdoptExchangeTimeout - time.Since(started)):
			}
			continue
		}
		if candidate == "" || candidate == current {
			continue
		}
		endpoint, err := peerEndpointFromCandidate(candidate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: %s: peer published unparseable candidate, will retry\n", peerLabel)
			continue
		}
		adoptBind(node, candidate, endpoint)
		if err := node.AddPeer(peerPubkey, allowedIP, &endpoint); err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: %s: adopting new candidate failed, will retry: %v\n", peerLabel, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "data-relay: %s adopted new endpoint %s\n", peerLabel, endpoint)
		current = candidate
	}
}

// peerEndpointFromCandidate picks the endpoint to dial from a peer's
// announcement. It is a dial hint, never an admission signal.
func peerEndpointFromCandidate(candidate string) (netip.AddrPort, error) {
	announcement, err := rendezvous.ParseAnnouncement(candidate)
	if err != nil {
		return netip.AddrPort{}, err
	}
	best, ok := announcement.Best()
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("announcement has no usable endpoint")
	}
	return best, nil
}
