// brambled is the per-node mesh networking daemon.
//
// Phase 1 Section A: spawn its own local ENS sidecar (docs/adr/0002) and
// resolve a real device subname through it (`resolve`).
// Phase 1 Section B: the admission verifier (Gate 1.1) — keep a WireGuard
// device's peer table synced to ENS state (`serve`).
// Phase 1 Section C: real OS TUN by default (Gate 0.2's "ping and one TCP
// connection across the tunnel" needs a genuine OS-visible interface, not
// just an in-process one — see brambled/wgnode) and rendezvous-relay
// candidate exchange (`-rendezvous`, see brambled/rendezvous and ../relay),
// so two nodes on different networks can find each other without a trusted
// coordinator. STUN/hole-punching for the harder two-NAT case is still not
// implemented — see docs/10_DAY0_GATES.md Gate 0.2's note on laptop-to-VPS
// being the easier, currently-supported topology.
// Phase 2 Section A: EAC-gated enrollment/revocation/rotation (Gate 2.1) —
// built in ../admincli, a separate package that writes to the registry
// directly rather than through any node's sidecar (see
// docs/adr/0004-admin-cli-writes-directly-to-registry.md). The only change
// on this side is admission.Loop's authorization rule, which now also
// checks a `revoked` text record (additive — the pre-existing clear-pubkey
// revocation path Gate 1.3 verified is untouched).
// Phase 2 Section C: gateway enforcement (Gate 2.3) — a fixed-port CONNECT
// listener (see ../gateway) started alongside the admission loop, checking a
// requester's on-chain acl digests against this node's own acl-granters
// before proxying to a local service (docs/adr/0005-acl-record-schema.md,
// docs/adr/0006-gateway-connect-protocol.md).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/datarelay"
	"github.com/WhyAsh5114/bramble/brambled/gateway"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/sidecar"
	"github.com/WhyAsh5114/bramble/brambled/statusapi"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "resolve":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: brambled resolve <device-label>")
			os.Exit(1)
		}
		if err := runResolve(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "demo-data-relay":
		if err := runDemoDataRelay(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "genkey":
		priv, pub, err := wgnode.GenerateKeyPair()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Printf("private: %s\n", priv)
		fmt.Printf("public:  %s\n", pub)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: brambled resolve <device-label>")
	fmt.Fprintln(os.Stderr, "       brambled serve -label <own-label> [-peer label=allowed-ip/prefix ...] [-service name=port ...] [-forward local-port=gateway-label:service ...] [-rendezvous host:port] [-netstack]")
	fmt.Fprintln(os.Stderr, "       brambled genkey")
	fmt.Fprintln(os.Stderr, "       brambled demo-data-relay [-rendezvous host:port] [-primary url] [-backup url] [-small-bytes n] [-large-bytes n] [-session-bytes n]")
}

func sidecarConfig() sidecar.Config {
	port := 7890
	if v := os.Getenv("BRAMBLE_SIDECAR_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	return sidecar.Config{
		Dir:             envOr("BRAMBLE_SIDECAR_DIR", "../sidecar"),
		Port:            port,
		TailnetName:     os.Getenv("BRAMBLE_TAILNET_NAME"),
		TailnetRegistry: os.Getenv("BRAMBLE_TAILNET_REGISTRY"),
		RPCURL:          os.Getenv("SEPOLIA_RPC_URL"),
		// docs/adr/0007's client-side payment config — all optional, only
		// needed when a relay in play is running with -meter.
		HederaClientAccountID:  os.Getenv("HEDERA_CLIENT_ACCOUNT_ID"),
		HederaClientPrivateKey: os.Getenv("HEDERA_CLIENT_PRIVATE_KEY"),
	}
}

func runResolve(label string) error {
	cfg := sidecarConfig()
	if cfg.TailnetName == "" || cfg.TailnetRegistry == "" {
		return fmt.Errorf("BRAMBLE_TAILNET_NAME and BRAMBLE_TAILNET_REGISTRY must be set")
	}

	fmt.Fprintln(os.Stderr, "starting sidecar...")
	m, err := sidecar.Start(cfg)
	if err != nil {
		return err
	}
	defer m.Stop()
	fmt.Fprintln(os.Stderr, "sidecar healthy, resolving...")

	record, err := m.ResolveDevice(label)
	if err != nil {
		return err
	}

	fmt.Printf("fullname: %s\n", record.Fullname)
	if record.Pubkey != nil {
		fmt.Printf("pubkey:   %s\n", *record.Pubkey)
	} else {
		fmt.Printf("pubkey:   (unset)\n")
	}
	fmt.Printf("status:   %d\n", record.Status)
	fmt.Printf("expiry:   %s\n", record.Expiry)
	fmt.Printf("tokenId:  %s\n", record.TokenID)
	return nil
}

// peerFlag accumulates repeated -peer label=allowed-ip/prefix flags.
type peerFlag []admission.Peer

func (p *peerFlag) String() string { return fmt.Sprintf("%v", []admission.Peer(*p)) }

func (p *peerFlag) Set(value string) error {
	label, cidr, ok := strings.Cut(value, "=")
	if !ok {
		return fmt.Errorf("expected label=allowed-ip/prefix, got %q", value)
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return fmt.Errorf("parsing allowed IP for %q: %w", label, err)
	}
	*p = append(*p, admission.Peer{Label: label, AllowedIP: prefix})
	return nil
}

// serviceFlag accumulates repeated -service name=port flags — this node's
// local, non-ENS service→port config (docs/adr/0005 Q1: never published on
// chain, mapped purely by the receiving gateway).
type serviceFlag map[string]uint16

func (s serviceFlag) String() string { return fmt.Sprintf("%v", map[string]uint16(s)) }

func (s serviceFlag) Set(value string) error {
	name, portStr, ok := strings.Cut(value, "=")
	if !ok {
		return fmt.Errorf("expected name=port, got %q", value)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("parsing port for service %q: %w", name, err)
	}
	s[name] = uint16(port)
	return nil
}

// forwardEntry is one parsed -forward flag: a local port that proxies to
// gateway-label's service over the tunnel, injecting the CONNECT preamble on
// behalf of whatever unmodified client connects locally (see
// brambled/gateway.Forwarder, docs/adr/0006-gateway-connect-protocol.md).
type forwardEntry struct {
	LocalPort    uint16
	GatewayLabel string
	Service      string
}

// forwardFlag accumulates repeated -forward local-port=gateway-label:service
// flags.
type forwardFlag []forwardEntry

func (f *forwardFlag) String() string { return fmt.Sprintf("%v", []forwardEntry(*f)) }

func (f *forwardFlag) Set(value string) error {
	portStr, rest, ok := strings.Cut(value, "=")
	if !ok {
		return fmt.Errorf("expected local-port=gateway-label:service, got %q", value)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("parsing local port in %q: %w", value, err)
	}
	label, service, ok := strings.Cut(rest, ":")
	if !ok || label == "" || service == "" {
		return fmt.Errorf("expected local-port=gateway-label:service, got %q", value)
	}
	*f = append(*f, forwardEntry{LocalPort: uint16(port), GatewayLabel: label, Service: service})
	return nil
}

// dataRelayFlag accumulates repeated -data-relay label=sidecar-url flags —
// the pool of candidate data relays brambled picks between for any peer
// flagged with -relay-peer (docs/adr/0008, "Relay selection").
type dataRelayFlag map[string]string

func (d dataRelayFlag) String() string { return fmt.Sprintf("%v", map[string]string(d)) }

func (d dataRelayFlag) Set(value string) error {
	label, sidecarURL, ok := strings.Cut(value, "=")
	if !ok || label == "" || sidecarURL == "" {
		return fmt.Errorf("expected label=sidecar-url, got %q", value)
	}
	d[label] = sidecarURL
	return nil
}

// relayPeerFlag accumulates repeated -relay-peer label flags: tracked peer
// labels that should use the data-relay pool above instead of publishing
// this node's own real candidate — the explicit, operator-chosen substitute
// for hole-punch-failure detection this project doesn't implement (see
// docs/adr/0008's "How does brambled decide to use the data relay?").
type relayPeerFlag map[string]bool

func (r relayPeerFlag) String() string { return fmt.Sprintf("%v", map[string]bool(r)) }

func (r relayPeerFlag) Set(value string) error {
	if value == "" {
		return fmt.Errorf("expected a tracked -peer label, got empty string")
	}
	r[value] = true
	return nil
}

// priceQuote mirrors relay-sidecar's GET /price response (docs/adr/0008).
type priceQuote struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

const dataRelayQuoteTimeout = 5 * time.Second

type dataRelayPurchasePolicy struct {
	MaxPricePerByte int64
	MaxSessionCost  int64
}

var unrestrictedDataRelayPurchasePolicy = dataRelayPurchasePolicy{
	MaxPricePerByte: 1<<63 - 1,
	MaxSessionCost:  1<<63 - 1,
}

func validateDataRelayQuote(q datarelay.Quote, bytes int64, policy dataRelayPurchasePolicy) error {
	if bytes <= 0 {
		return fmt.Errorf("session bytes must be positive")
	}
	if policy.MaxPricePerByte <= 0 || policy.MaxSessionCost <= 0 {
		return fmt.Errorf("relay purchase limits must be positive")
	}
	if q.PricePerByte <= 0 {
		return fmt.Errorf("price must be positive")
	}
	if q.PricePerByte > policy.MaxPricePerByte {
		return fmt.Errorf("price %d exceeds per-byte limit %d", q.PricePerByte, policy.MaxPricePerByte)
	}
	// Division avoids overflowing price*bytes for an untrusted quote.
	if q.PricePerByte > policy.MaxSessionCost/bytes {
		return fmt.Errorf("session cost exceeds limit %d atomic USDC", policy.MaxSessionCost)
	}
	return nil
}

// quoteDataRelay fetches label's advertised per-byte price and measures the
// round trip as a latency proxy — both inputs to datarelay.Pick's
// composite score (docs/adr/0008, "Relay selection").
func quoteDataRelay(label, sidecarURL string) (datarelay.Quote, error) {
	relayURL, err := url.Parse(sidecarURL)
	if err != nil || (relayURL.Scheme != "http" && relayURL.Scheme != "https") || relayURL.Host == "" {
		return datarelay.Quote{}, fmt.Errorf("%s has an invalid HTTP(S) sidecar URL", label)
	}
	if relayURL.User != nil || relayURL.RawQuery != "" || relayURL.Fragment != "" {
		return datarelay.Quote{}, fmt.Errorf("%s sidecar URL must not contain credentials, a query, or a fragment", label)
	}
	start := time.Now()
	client := &http.Client{
		Timeout: dataRelayQuoteTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(strings.TrimRight(sidecarURL, "/") + "/price")
	latency := time.Since(start)
	if err != nil {
		return datarelay.Quote{}, fmt.Errorf("querying %s price: %w", label, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return datarelay.Quote{}, fmt.Errorf("%s returned HTTP %d for /price", label, resp.StatusCode)
	}
	var q priceQuote
	if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
		return datarelay.Quote{}, fmt.Errorf("decoding %s price quote: %w", label, err)
	}
	price, err := strconv.ParseInt(q.Amount, 10, 64)
	if err != nil {
		return datarelay.Quote{}, fmt.Errorf("%s price %q is not an integer: %w", label, q.Amount, err)
	}
	if q.Asset != "0.0.429274" {
		return datarelay.Quote{}, fmt.Errorf("%s quoted unsupported asset %q; expected Hedera testnet USDC 0.0.429274", label, q.Asset)
	}
	if price <= 0 {
		return datarelay.Quote{}, fmt.Errorf("%s quoted non-positive price %d", label, price)
	}
	return datarelay.Quote{Label: label, SidecarURL: sidecarURL, PricePerByte: price, LatencyMS: latency.Milliseconds()}, nil
}

// dataRelaySession is what buyDataRelaySession hands back to the
// EndpointResolver closure — everything it needs to log and to build the
// candidate string it publishes in place of this node's real address.
type dataRelaySession struct {
	id             string
	relayLabel     string
	candidate      string
	settlementTxID string
}

// buyDataRelaySession queries every candidate in pool for a price quote,
// picks the best via datarelay.Pick, buys a session from it through the
// local sidecar, and derives the relay's public data-relay candidate
// address from the winning sidecar URL's hostname — same-machine
// co-location between a relay-sidecar and its Go relay is an assumed,
// documented deployment convention (docs/adr/0008), not something
// discovered dynamically.
func buyDataRelaySession(m *sidecar.Manager, pool dataRelayFlag, bytes int64, policy dataRelayPurchasePolicy) (*dataRelaySession, error) {
	if bytes <= 0 {
		return nil, fmt.Errorf("data-relay session bytes must be positive")
	}
	quotes := make([]datarelay.Quote, 0, len(pool))
	for label, sidecarURL := range pool {
		q, err := quoteDataRelay(label, sidecarURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: skipping %s: %v\n", label, err)
			continue
		}
		if err := validateDataRelayQuote(q, bytes, policy); err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: skipping %s: %v\n", label, err)
			continue
		}
		quotes = append(quotes, q)
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("no -data-relay candidate returned a usable price quote")
	}

	winner := datarelay.Pick(quotes)
	resp, err := m.DataRelaySession(winner.SidecarURL, bytes)
	if err != nil {
		return nil, fmt.Errorf("buying session from %s: %w", winner.Label, err)
	}

	relayURL, err := url.Parse(winner.SidecarURL)
	if err != nil {
		return nil, fmt.Errorf("parsing %s sidecar URL %q: %w", winner.Label, winner.SidecarURL, err)
	}

	return &dataRelaySession{
		id:             resp.SessionID,
		relayLabel:     winner.Label,
		candidate:      net.JoinHostPort(relayURL.Hostname(), strconv.Itoa(resp.Port)),
		settlementTxID: resp.SettlementTxID,
	}, nil
}

// dataRelayKeepaliveSeconds governs how often a relay-routed peer sends an
// unsolicited packet to the relay (see relayRoutedEndpoint's SetPersistentKeepalive
// call) — needs to be well under the data-relay session TTL and short
// enough for a live demo to feel responsive, not tuned for production NAT
// mapping lifetimes (docs/adr/0008).
const dataRelayKeepaliveSeconds = 5

// keepaliveSetter is satisfied by *wgnode.Node — narrowed so
// relayRoutedEndpoint's signature only asks for what it actually uses.
type keepaliveSetter interface {
	SetPersistentKeepalive(publicKeyHex string, seconds int) error
}

// relayRoutedEndpoint is the EndpointResolver path for a -relay-peer
// (docs/adr/0008). It buys a session, then returns the relay's own address
// as this node's peer-endpoint — never the peer's candidate from Exchange's
// return value. That distinction matters: both sides of a relay-routed
// connection must end up pointed at the identical relay socket for the
// relay's address-learning to fill both of its slots, and the peer's own
// published candidate (which could be "" for a peer with no direct address
// at all — the exact topology -relay-peer exists for) is irrelevant to
// where this node sends next. Exchange is still called so the peer learns
// this node's candidate (the relay address) the normal way.
//
// It also arms a persistent keepalive on this peer before returning: the
// relay only learns an address from outbound traffic it observes, but
// WireGuard's own protocol never has the passive/responder side send
// anything on its own — without this, whichever side doesn't happen to
// initiate the real handshake would never register with the relay at all,
// and nothing would ever forward in that direction. Calling it here, before
// admission.Loop's own AddPeer runs, is safe and deterministic: WireGuard's
// UAPI merges fields into a peer entry by public key rather than requiring
// them all in one call.
// Returns the relay label actually used, alongside the endpoint — Section
// D's failover watcher (below) needs to know which relay a peer is
// currently routed through, so it can exclude that one when picking a
// replacement.
func relayRoutedEndpoint(node keepaliveSetter, m *sidecar.Manager, pool dataRelayFlag, sessionBytes int64, policy dataRelayPurchasePolicy, relays []sidecar.RendezvousRelay, own, peerPubkey string, ttl time.Duration) (*netip.AddrPort, string, error) {
	session, err := buyDataRelaySession(m, pool, sessionBytes, policy)
	if err != nil {
		return nil, "", fmt.Errorf("buying data-relay session: %w", err)
	}
	relayEndpoint, err := netip.ParseAddrPort(session.candidate)
	if err != nil {
		return nil, "", fmt.Errorf("data-relay candidate %q unparseable: %w", session.candidate, err)
	}
	if err := node.SetPersistentKeepalive(peerPubkey, dataRelayKeepaliveSeconds); err != nil {
		return nil, "", fmt.Errorf("arming persistent keepalive for %s: %w", peerPubkey, err)
	}
	fmt.Fprintf(os.Stderr, "data-relay: bought session %s from %s (tx %s), routing %s via %s\n",
		session.id, session.relayLabel, session.settlementTxID, peerPubkey, session.candidate)
	if _, _, err := exchangeAnyMetered(relays, m, own, peerPubkey, session.candidate, ttl); err != nil {
		return nil, "", err
	}
	return &relayEndpoint, session.relayLabel, nil
}

// Section D failover (docs/adr/0008, docs/05_BUILD_PLAN.md Gate 4.3): how
// often to poll RxBytes, and how long it must sit still before a relay is
// declared stalled. Deliberately fast for a live demo, not tuned for
// production — a real deployment would want a longer stallTimeout to avoid
// false positives during ordinary idle gaps outside an active transfer.
const (
	dataRelayPollInterval = 2 * time.Second
	dataRelayStallTimeout = 6 * time.Second
)

// relayState is the shared record of which data relay each -relay-peer is
// currently routed through, and under which pubkey — set by
// relayRoutedEndpoint (via the EndpointResolver closure) on every successful
// connect, and read by watchDataRelayFailover to know what to watch and
// what to exclude on failover. Keyed by tracked-peer *label*, not pubkey,
// so a key rotation naturally carries over: EndpointResolver runs again
// under the new pubkey (admission.Loop's resolvedEndpoints cache is keyed
// by pubkey), which just overwrites this label's entry.
type relayState struct {
	mu     sync.Mutex
	pubkey map[string]string
	relay  map[string]string
}

func newRelayState() *relayState {
	return &relayState{pubkey: make(map[string]string), relay: make(map[string]string)}
}

func (s *relayState) set(peerLabel, pubkey, relayLabel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pubkey[peerLabel] = pubkey
	s.relay[peerLabel] = relayLabel
}

func (s *relayState) get(peerLabel string) (pubkey, relayLabel string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pubkey, hasPubkey := s.pubkey[peerLabel]
	relayLabel, hasRelay := s.relay[peerLabel]
	return pubkey, relayLabel, hasPubkey && hasRelay
}

// setPubkey records peerLabel's current pubkey without touching the relay
// map — used for peers that aren't -relay-peer (they never call set(),
// since they never buy a session), so watchDataRelayAdopt below still has
// something to poll for once EndpointResolver has resolved them at least
// once.
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

// exchangeAnyMetered is the rendezvous-set failover docs/adr/0003 requires,
// fixed to pair each relay with a token bought from *that relay's own*
// sidecar rather than one token tried everywhere (docs/adr/0008): a relay
// running with -meter verifies hellos against a secret only its own
// relay-sidecar knows (relay/sidecar.go generates a fresh random one per
// relay process), so a token minted for relay A can never verify at relay
// B. Tries every relay in relays concurrently, each buying its own token
// from its SidecarURL first if it has one, then calling rendezvous.Exchange
// against its Address with the *full* exchangeTimeout — returning as soon
// as any one succeeds, alongside the label of the relay actually used.
//
// Concurrent, not sequential-with-a-split-timeout: a first version tried
// relays one at a time with exchangeTimeout/len(relays) each, and a live
// run plus a two-relay regression test both caught the same failure mode —
// whichever relay came later in the list got starved to a fraction of the
// budget, and worse, a caller that re-dials every relay every cycle (like
// watchDataRelayAdopt) spent part of each cycle connected to an earlier,
// dead relay instead of continuously present at the one that actually
// works, missing the peer's own once-a-second retry window on the relay
// that mattered. Every relay getting the full timeout, all at once, closes
// that gap — and costs the same in token purchases as the sequential
// version already did (a dead relay's token still got bought before it was
// dialed, either way).
//
// A token is bought fresh on every attempt, never cached or reused: a
// relay's own tokenVerifier (relay/token.go) marks each token's nonce
// consumed on first use and rejects it on a second presentation, matching
// rendezvous.Exchange's own documented contract of "one rendezvous-token
// per Exchange call" (docs/adr/0007) — reuse isn't an optimization here,
// it's a guaranteed failure the second time. If a caller needs to spend
// less, the fix is a slower loop cycle, not a cached token.
func exchangeAnyMetered(relays []sidecar.RendezvousRelay, m *sidecar.Manager, ownPubkey, peerPubkey, myCandidate string, exchangeTimeout time.Duration) (candidate, usedLabel string, err error) {
	if len(relays) == 0 {
		return "", "", fmt.Errorf("no rendezvous relays to try")
	}

	type attemptResult struct {
		candidate, label string
		err              error
	}
	results := make(chan attemptResult, len(relays))
	for _, r := range relays {
		go func(r sidecar.RendezvousRelay) {
			var token string
			if r.SidecarURL != "" {
				resp, tokErr := m.RendezvousToken(r.SidecarURL)
				if tokErr != nil {
					results <- attemptResult{label: r.Label, err: fmt.Errorf("buying rendezvous token: %w", tokErr)}
					return
				}
				token = resp.Token
				if resp.SettlementTxID != "" {
					fmt.Fprintf(os.Stderr, "rendezvous: paid %s for a rendezvous token (tx %s)\n", r.Label, resp.SettlementTxID)
				}
			}
			candidate, err := rendezvous.Exchange(r.Address, ownPubkey, peerPubkey, myCandidate, token, exchangeTimeout)
			results <- attemptResult{candidate: candidate, label: r.Label, err: err}
		}(r)
	}

	var lastErr error
	for range relays {
		res := <-results
		if res.err == nil {
			return res.candidate, res.label, nil
		}
		fmt.Fprintf(os.Stderr, "rendezvous: %s: exchange failed: %v\n", res.label, res.err)
		lastErr = res.err
	}
	return "", "", lastErr
}

// watchDataRelayFailover is Gate 4.3's kill-mid-transfer failover: once
// peerLabel's initial data-relay connect has happened (state.get succeeds),
// it blocks on datarelay.WaitForStall and, on a real stall, buys a fresh
// session from the pool minus whichever relay just stalled, re-points this
// node's own peer-endpoint directly (admission.Loop won't — its
// EndpointResolver only fires once per pubkey for the life of the Loop),
// and updates state so the next round excludes the new relay too if it
// also fails. Gives up (logs and returns) if the pool is exhausted or a
// reconnect attempt itself fails — matches this file's existing "log and
// move on" posture for discovery/pricing failures elsewhere.
func watchDataRelayFailover(ctx context.Context, node *wgnode.Node, m *sidecar.Manager, state *relayState, peerLabel string, allowedIP netip.Prefix, pool dataRelayFlag, sessionBytes int64, policy dataRelayPurchasePolicy, relays []sidecar.RendezvousRelay, ownPubkey string, ttl time.Duration) {
	// Every relay ever excluded, not just the one currently in use —
	// without this, a second stall (e.g. the just-adopted backup relay
	// also looking dead because the *other* side hasn't picked it up yet)
	// recomputes "pool minus usedLabel" from scratch and retries whichever
	// relay stalled first, forever, since usedLabel is the only thing that
	// changed. Found by a live run and a test whose fake relay didn't stay
	// dead across retries (docs/adr/0008).
	excluded := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		pubkey, usedLabel, ok := state.get(peerLabel)
		if !ok {
			continue // initial connect hasn't happened yet
		}

		if err := datarelay.WaitForStall(ctx, node, pubkey, dataRelayPollInterval, dataRelayStallTimeout); err != nil {
			return // ctx cancelled
		}
		fmt.Fprintf(os.Stderr, "data-relay: %s via %s stalled — failing over\n", peerLabel, usedLabel)
		excluded[usedLabel] = true

		remaining := dataRelayFlag{}
		for label, url := range pool {
			if !excluded[label] {
				remaining[label] = url
			}
		}
		if len(remaining) == 0 {
			fmt.Fprintf(os.Stderr, "data-relay: %s: no relay left to fail over to (excluded %s), giving up\n", peerLabel, usedLabel)
			return
		}

		// A failed attempt here is very often just timing skew, not a dead
		// end: the peer's own watcher may not have declared the same
		// stall yet, so this side's exchangeAnyMetered (which needs the
		// peer concurrently exchanging too) can time out even though the
		// pool still has a perfectly good relay left. Log and retry rather
		// than giving up — WaitForStall's own stallTimeout below is what
		// rate-limits the retry, since usedLabel is unchanged and RxBytes
		// still isn't moving. Only pool exhaustion above is a real reason
		// to stop.
		endpoint, newLabel, err := relayRoutedEndpoint(node, m, remaining, sessionBytes, policy, relays, ownPubkey, pubkey, ttl)
		if err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: %s: failover attempt failed, will retry: %v\n", peerLabel, err)
			continue
		}
		if err := node.AddPeer(pubkey, allowedIP, endpoint); err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: %s: re-pointing peer at %s failed, will retry: %v\n", peerLabel, newLabel, err)
			continue
		}
		state.set(peerLabel, pubkey, newLabel)
		fmt.Fprintf(os.Stderr, "data-relay: %s failed over to %s\n", peerLabel, newLabel)
	}
}

// watchDataRelayAdopt is the non-buying side's half of Section D failover
// (docs/adr/0008): this node never buys a data-relay session for
// peerLabel — it just adopts whatever candidate the peer publishes, same
// as the initial connect (EndpointResolver's non-relay-peer branch).
// Nothing else re-learns a peer's new address after that peer fails over,
// since admission.Loop's EndpointResolver only fires once per pubkey for
// the life of the Loop — this watcher is what makes that not a dead end.
// It watches the same real RxBytes signal watchDataRelayFailover does and,
// on a stall, re-runs the plain candidate exchange and re-points, mirroring
// runServe's own non-relay-peer EndpointResolver branch exactly.
// dataRelayAdoptExchangeTimeout is every relay's Exchange budget per cycle
// (exchangeAnyMetered gives each relay this in full, run concurrently —
// see its own doc comment for why sequential-with-a-split-budget measured
// worse), deliberately short unlike the buying side's own exchange calls:
// Exchange is one-shot rendezvous (it returns the *first* offer that
// arrives while its own offer gets acked), not a subscription — a
// long-lived call here could sit open across several of the peer's own
// re-publishes and end up adopting a stale one, whichever happened to be
// in flight when its own ack finally landed. Short calls mean this side
// re-registers every few seconds instead, so the peer's next re-publish
// reliably lands inside a live window instead of an already-stale one
// (found via a live run and a test that only worked when the fake relay's
// ports were coincidentally identical across calls — see docs/adr/0008).
const dataRelayAdoptExchangeTimeout = 3 * time.Second

// watchDataRelayAdopt does not gate on WaitForStall the way
// watchDataRelayFailover does: it stays continuously present at the relay
// instead, cycling a short Exchange every few seconds for as long as ctx
// lives. Tested this way, not gated on its own stall detection — Exchange
// is one-shot rendezvous, not a subscription, so this side has to keep
// re-registering to reliably catch the peer's next re-publish regardless
// of when its own RxBytes happens to stall. A peer that is genuinely still
// connected keeps re-exchanging the same candidate here too — harmless,
// since AddPeer only runs again when the candidate actually changes.
func watchDataRelayAdopt(ctx context.Context, node *wgnode.Node, m *sidecar.Manager, peerLabel, ownPubkey, peerPubkey, ownCandidate string, allowedIP netip.Prefix, relays []sidecar.RendezvousRelay) {
	var current string
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		candidate, _, err := exchangeAnyMetered(relays, m, ownPubkey, peerPubkey, ownCandidate, dataRelayAdoptExchangeTimeout)
		if err != nil || candidate == "" || candidate == current {
			continue
		}
		endpoint, err := netip.ParseAddrPort(candidate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: %s: peer published unparseable candidate %q, will retry\n", peerLabel, candidate)
			continue
		}
		// This node has no way to know peerPubkey's new candidate is
		// relay-routed rather than a real direct address, so this has to
		// be unconditional — the relay only learns an address from
		// outbound traffic it observes (docs/adr/0008).
		if err := node.SetPersistentKeepalive(peerPubkey, dataRelayKeepaliveSeconds); err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: %s: arming persistent keepalive failed, will retry: %v\n", peerLabel, err)
			continue
		}
		if err := node.AddPeer(peerPubkey, allowedIP, &endpoint); err != nil {
			fmt.Fprintf(os.Stderr, "data-relay: %s: adopting new candidate failed, will retry: %v\n", peerLabel, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "data-relay: %s adopted new candidate %s\n", peerLabel, candidate)
		current = candidate
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var peers peerFlag
	fs.Var(&peers, "peer", "peer to track: label=allowed-ip/prefix (repeatable)")
	services := serviceFlag{}
	fs.Var(services, "service", "local service this node offers as a gateway: name=local-port (repeatable); omit to offer none (docs/adr/0006-gateway-connect-protocol.md)")
	var forwards forwardFlag
	fs.Var(&forwards, "forward", "local port-forward to a peer's gateway service: local-port=gateway-label:service (repeatable) — lets an unmodified local client (psql, curl, ...) reach an ACL'd mesh service with no CONNECT knowledge of its own (docs/adr/0006-gateway-connect-protocol.md)")
	localPrefix := fs.String("local-addr", "10.77.0.1/24", "this node's address and mesh subnet (CIDR)")
	listenPort := fs.Uint("listen-port", 51820, "UDP port this node's WireGuard transport binds to")
	ttl := fs.Duration("ttl", 30*time.Second, "how often to re-resolve ENS state for tracked peers")
	maxStale := fs.Duration("max-stale", 2*time.Minute, "remove a peer if resolving it keeps failing for this long (sidecar/RPC down) instead of leaving it admitted indefinitely; defaults to a bounded 2 minutes rather than unbounded fail-open — pass 0 explicitly to disable this and fail open forever (see admission package doc)")
	keepaliveSeconds := fs.Int("keepalive", 0, "send an unsolicited WireGuard packet to every authorized peer this often (seconds) so NAT mappings/roaming state don't go stale on an otherwise-idle connection; 0 disables this (existing behavior — a data-relay pool already arms its own keepalive independently of this flag, see dataRelayKeepaliveSeconds)")
	privateKeyHex := fs.String("private-key", os.Getenv("BRAMBLE_PRIVATE_KEY"), "hex-encoded WireGuard private key (generated ephemerally if unset — not persisted)")
	ownLabel := fs.String("label", "", "this node's own ENS label — required to resolve its own acl-granters record for gateway enforcement (docs/adr/0005-acl-record-schema.md)")
	gatewayPort := fs.Uint("gateway-port", 7892, "fixed TCP port this node's CONNECT gateway listener binds to on its own tunnel address (7892, not 7891 — that's relay-sidecar's default port, and both can end up on the same machine during local dev/testing, docs/adr/0008)")
	statusPort := fs.Uint("status-port", 7899, "port for this node's own status API (live WireGuard peer state, ping, recent admission/gateway activity) — read-only diagnostics for local tooling like the dashboard, see brambled/statusapi")
	statusBind := fs.String("status-bind", "127.0.0.1", "bind address for the status API — loopback-only by default; set to 0.0.0.0 only if a dashboard on a different machine genuinely needs to reach this node's status API remotely")
	interfaceName := fs.String("interface", "", "real OS interface name (auto-picked if unset: \"utun\" on macOS, \"bramble0\" on linux)")
	netstackMode := fs.Bool("netstack", false, "use a virtual (gVisor) TUN instead of a real OS interface — testing/local dev only, never the demo")
	rendezvousAddr := fs.String("rendezvous", "", "rendezvous relay address (host:port) for candidate exchange; omit for static/dial-in-first peers only")
	myCandidate := fs.String("my-candidate", "", "this node's own reachable address (ip:port) to publish via the rendezvous relay, e.g. a VPS's public IP:listen-port")
	dataRelays := dataRelayFlag{}
	fs.Var(dataRelays, "data-relay", "candidate data-plane relay: label=relay-sidecar-base-url (repeatable) — the pool -relay-peer picks from (docs/adr/0008)")
	relayPeers := relayPeerFlag{}
	fs.Var(relayPeers, "relay-peer", "tracked -peer label that should use the data-relay pool instead of a direct candidate (repeatable) — an explicit substitute for hole-punch-failure detection, which this project doesn't implement (docs/adr/0008)")
	relaySessionBytes := fs.Int64("relay-session-bytes", 10_000, "byte allotment to buy per data-relay session (docs/adr/0008) — size this to the transfer you're about to demo; running out mid-transfer needs a fresh session, not an automatic top-up. At the -price-per-byte default (1 atomic USDC/byte), the default here is 0.01 USDC per session — raise deliberately, since cost scales linearly with this")
	maxRelayPricePerByte := fs.Int64("max-relay-price-per-byte", 10, "maximum acceptable data-relay price in atomic USDC per byte")
	maxRelaySessionCost := fs.Int64("max-relay-session-cost", 100_000, "maximum acceptable data-relay session cost in atomic USDC (0.10 USDC by default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(peers) == 0 {
		return fmt.Errorf("at least one -peer label=allowed-ip/prefix is required")
	}
	if *relaySessionBytes <= 0 || *maxRelayPricePerByte <= 0 || *maxRelaySessionCost <= 0 {
		return fmt.Errorf("relay session bytes and relay price/cost limits must all be positive")
	}
	relayPurchasePolicy := dataRelayPurchasePolicy{
		MaxPricePerByte: *maxRelayPricePerByte,
		MaxSessionCost:  *maxRelaySessionCost,
	}
	if *ownLabel == "" {
		return fmt.Errorf("-label (this node's own ENS label) is required")
	}
	peerAddrByLabel := make(map[string]netip.Addr, len(peers))
	for _, p := range peers {
		peerAddrByLabel[p.Label] = p.AllowedIP.Addr()
	}
	for _, fwd := range forwards {
		if _, ok := peerAddrByLabel[fwd.GatewayLabel]; !ok {
			return fmt.Errorf("-forward %d=%s:%s: %q is not a tracked -peer — a forward target must be a peer this node admits", fwd.LocalPort, fwd.GatewayLabel, fwd.Service, fwd.GatewayLabel)
		}
	}
	for label := range relayPeers {
		if _, ok := peerAddrByLabel[label]; !ok {
			return fmt.Errorf("-relay-peer %q is not a tracked -peer", label)
		}
	}
	// The len(dataRelays)==0 case is checked further below, after relay
	// discovery has had a chance to populate the pool — -data-relay isn't
	// the only source of it anymore (docs/adr/0003's Consequence section).

	prefix, err := netip.ParsePrefix(*localPrefix)
	if err != nil {
		return fmt.Errorf("parsing -local-addr: %w", err)
	}

	if *privateKeyHex == "" {
		generated, _, genErr := wgnode.GenerateKeyPair()
		if genErr != nil {
			return fmt.Errorf("generating ephemeral private key: %w", genErr)
		}
		*privateKeyHex = generated
		fmt.Fprintln(os.Stderr, "no -private-key/BRAMBLE_PRIVATE_KEY set — generated an ephemeral one (not persisted; this node's identity changes every restart)")
	}
	ownPubkey, err := wgnode.PublicKeyFromPrivateHex(*privateKeyHex)
	if err != nil {
		return fmt.Errorf("deriving own public key: %w", err)
	}

	scfg := sidecarConfig()
	if scfg.TailnetName == "" || scfg.TailnetRegistry == "" {
		return fmt.Errorf("BRAMBLE_TAILNET_NAME and BRAMBLE_TAILNET_REGISTRY must be set")
	}

	fmt.Fprintln(os.Stderr, "starting sidecar...")
	m, err := sidecar.Start(scfg)
	if err != nil {
		return err
	}
	defer m.Stop()

	// Real ENS-based relay discovery (docs/adr/0003's Consequence section,
	// resolved) — -rendezvous/-data-relay stay as explicit overrides
	// (static config wins outright, same precedent as admission.Peer.Endpoint
	// overriding EndpointResolver): only queried when the corresponding flag
	// was left unset. A relay registry that's unconfigured or unreachable
	// just yields empty lists here (m.Relays()'s own documented behavior),
	// which is indistinguishable from "no relays available" — non-fatal,
	// same fail-open-on-discovery posture the rest of this file already has.
	rendezvousRelays := []sidecar.RendezvousRelay{}
	if *rendezvousAddr != "" {
		// BRAMBLE_RENDEZVOUS_PAYMENT_URL is this one static relay's own
		// payment sidecar (docs/adr/0007) — empty is fine, meaning this
		// relay is unmetered (exchangeAnyMetered skips buying a token).
		rendezvousRelays = []sidecar.RendezvousRelay{{
			Label:      "static",
			Address:    *rendezvousAddr,
			SidecarURL: os.Getenv("BRAMBLE_RENDEZVOUS_PAYMENT_URL"),
		}}
	}
	if len(dataRelays) == 0 || len(rendezvousRelays) == 0 {
		if relays, relErr := m.Relays(); relErr != nil {
			fmt.Fprintf(os.Stderr, "relay discovery: %v (falling back to -rendezvous/-data-relay only)\n", relErr)
		} else {
			if len(rendezvousRelays) == 0 {
				for _, r := range relays.Rendezvous {
					rendezvousRelays = append(rendezvousRelays, sidecar.RendezvousRelay{Label: r.Label, Address: r.Address, SidecarURL: r.SidecarURL})
				}
			}
			if len(dataRelays) == 0 {
				for _, r := range relays.DataRelays {
					dataRelays[r.Label] = r.SidecarURL
				}
			}
		}
	}
	if len(relayPeers) > 0 && len(dataRelays) == 0 {
		return fmt.Errorf("-relay-peer requires at least one data relay, from -data-relay or discovery")
	}

	node, err := wgnode.New(wgnode.Config{
		PrivateKeyHex: *privateKeyHex,
		ListenPort:    uint16(*listenPort),
		LocalAddress:  prefix.Addr(),
		LocalPrefix:   prefix,
		InterfaceName: *interfaceName,
		Netstack:      *netstackMode,
	})
	if err != nil {
		return fmt.Errorf("starting WireGuard node: %w", err)
	}
	defer node.Close()

	// Tracks which data relay each -relay-peer is currently routed
	// through — read by the EndpointResolver closure below on every
	// connect, and by watchDataRelayFailover's background goroutines
	// (started further down, once ctx exists) for Gate 4.3's kill-mid-
	// transfer failover (docs/adr/0008).
	relState := newRelayState()

	// status is this node's own read-only diagnostic surface (live
	// WireGuard peer state, ping, recent admission/gateway activity) — the
	// dashboard's source for everything ENS/Hedera state alone can't show.
	// It has no write path of its own: OnAdmissionEvent/OnGatewayDecision
	// only ever record what the admission loop and gateway already decided.
	status := &statusapi.Server{PeerTable: node}

	loop := &admission.Loop{
		Resolver: m,
		Table:    node,
		Peers:    peers,
		TTL:      *ttl,
		MaxStale: *maxStale,
		OnEvent: func(e admission.Event) {
			switch {
			case e.Err != nil:
				fmt.Fprintf(os.Stderr, "admission: %s: error: %v\n", e.Label, e.Err)
			case e.Authorized:
				fmt.Fprintf(os.Stderr, "admission: %s: authorized (pubkey %s)\n", e.Label, e.PublicKey)
			default:
				fmt.Fprintf(os.Stderr, "admission: %s: not authorized\n", e.Label)
			}
			if e.EndpointErr != nil {
				fmt.Fprintf(os.Stderr, "admission: %s: endpoint exchange failed (will retry next sync): %v\n", e.Label, e.EndpointErr)
			}
			if e.Authorized && e.PublicKey != "" && *keepaliveSeconds > 0 {
				if err := node.SetPersistentKeepalive(e.PublicKey, *keepaliveSeconds); err != nil {
					fmt.Fprintf(os.Stderr, "admission: %s: arming keepalive failed, will retry next sync: %v\n", e.Label, err)
				}
			}
			status.OnAdmissionEvent(e)
		},
	}

	if len(rendezvousRelays) > 0 {
		relays, own, defaultCandidate := rendezvousRelays, ownPubkey, *myCandidate
		loop.EndpointResolver = func(p admission.Peer, peerPubkey string) (*netip.AddrPort, error) {
			if relayPeers[p.Label] {
				endpoint, label, err := relayRoutedEndpoint(node, m, dataRelays, *relaySessionBytes, relayPurchasePolicy, relays, own, peerPubkey, *ttl)
				if err == nil {
					relState.set(p.Label, peerPubkey, label)
				}
				return endpoint, err
			}
			// Recorded even for non-relay peers: watchDataRelayAdopt below
			// needs a peerPubkey to watch, and EndpointResolver is the only
			// place that ever learns one.
			relState.setPubkey(p.Label, peerPubkey)

			// exchangeAnyMetered is the rendezvous-set failover
			// docs/adr/0003 requires: one rogue or unreachable relay among
			// the (possibly discovered) set can't unilaterally block this
			// connection. Buys a fresh (or cached) token per relay it
			// tries, from that relay's own sidecar — docs/adr/0008.
			candidate, _, err := exchangeAnyMetered(relays, m, own, peerPubkey, defaultCandidate, *ttl)
			if err != nil {
				return nil, err
			}
			if candidate == "" {
				return nil, nil // peer published no reachable address of its own
			}
			endpoint, err := netip.ParseAddrPort(candidate)
			if err != nil {
				return nil, fmt.Errorf("peer %s published an unparseable candidate %q: %w", peerPubkey, candidate, err)
			}
			// This node has no way to know peerPubkey's candidate is
			// relay-routed rather than a real direct address — a relay
			// only learns an address from outbound traffic it observes
			// (docs/adr/0008), so if a data-relay pool is even in play on
			// this node at all, arm the keepalive defensively. Harmless
			// for a genuinely direct peer (a standard, common WireGuard
			// setting), required for one that turns out to be relay-routed
			// on the other end.
			if len(dataRelays) > 0 {
				if err := node.SetPersistentKeepalive(peerPubkey, dataRelayKeepaliveSeconds); err != nil {
					return nil, fmt.Errorf("arming persistent keepalive for %s: %w", peerPubkey, err)
				}
			}
			return &endpoint, nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	peerLabels := make(map[netip.Addr]string, len(peers))
	for _, p := range peers {
		peerLabels[p.AllowedIP.Addr()] = p.Label
	}

	// Gate 4.3 failover (docs/adr/0008): one watcher per -relay-peer,
	// started once ctx exists so Ctrl+C stops them cleanly. Each blocks
	// until relState reports an initial connect, then waits for a stall.
	for _, p := range peers {
		if !relayPeers[p.Label] {
			continue
		}
		p := p
		go watchDataRelayFailover(ctx, node, m, relState, p.Label, p.AllowedIP, dataRelays, *relaySessionBytes, relayPurchasePolicy, rendezvousRelays, ownPubkey, *ttl)
	}

	// The other half of Gate 4.3: every tracked peer that ISN'T a
	// -relay-peer on this node still needs a way to notice if *it's* the
	// one relay-routed on the other end and just failed over — see
	// watchDataRelayAdopt's doc comment. Scoped to deployments that opted
	// into data-relay routing at all (a data-relay pool + rendezvous set),
	// so a plain direct-peer setup with no relay involved anywhere keeps
	// its old behavior exactly (no extra re-exchange traffic on an
	// ordinary idle connection).
	if len(dataRelays) > 0 && len(rendezvousRelays) > 0 {
		for _, p := range peers {
			if relayPeers[p.Label] {
				continue
			}
			p := p
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Second):
					}
					pubkey, ok := relState.getPubkey(p.Label)
					if !ok {
						continue
					}
					watchDataRelayAdopt(ctx, node, m, p.Label, ownPubkey, pubkey, *myCandidate, p.AllowedIP, rendezvousRelays)
					return
				}
			}()
		}
	}

	var gwListener net.Listener
	if *netstackMode {
		gwListener, err = node.ListenTCP(uint16(*gatewayPort))
	} else {
		gwListener, err = net.Listen("tcp", net.JoinHostPort(prefix.Addr().String(), strconv.Itoa(int(*gatewayPort))))
	}
	if err != nil {
		return fmt.Errorf("starting gateway listener on port %d: %w", *gatewayPort, err)
	}
	defer gwListener.Close()

	gw := &gateway.Server{
		Listener:      gwListener,
		Resolver:      m,
		PrivateKeyHex: *privateKeyHex,
		OwnLabel:      *ownLabel,
		Services:      services,
		PeerLabels:    peerLabels,
		OnDecision:    status.OnGatewayDecision,
	}
	go func() {
		if err := gw.Serve(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "gateway: listener stopped: %v\n", err)
		}
	}()
	fmt.Fprintf(os.Stderr, "gateway: listening on %s:%d, offering %d service(s)\n", prefix.Addr(), *gatewayPort, len(services))

	go func() {
		if err := status.ListenAndServe(ctx, *statusBind, *statusPort); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "status: listener stopped: %v\n", err)
		}
	}()
	fmt.Fprintf(os.Stderr, "status: listening on %s:%d\n", *statusBind, *statusPort)

	for _, fwd := range forwards {
		fwd := fwd
		gatewayAddr := peerAddrByLabel[fwd.GatewayLabel]

		localLn, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(fwd.LocalPort))))
		if err != nil {
			return fmt.Errorf("starting forward listener for -forward %d=%s:%s: %w", fwd.LocalPort, fwd.GatewayLabel, fwd.Service, err)
		}
		defer localLn.Close()

		var dialTunnel func() (net.Conn, error)
		if *netstackMode {
			dialTunnel = func() (net.Conn, error) {
				return node.DialTCP(netip.AddrPortFrom(gatewayAddr, uint16(*gatewayPort)))
			}
		} else {
			target := net.JoinHostPort(gatewayAddr.String(), strconv.Itoa(int(*gatewayPort)))
			dialTunnel = func() (net.Conn, error) {
				return net.Dial("tcp", target)
			}
		}

		f := &gateway.Forwarder{
			Listener:   localLn,
			DialTunnel: dialTunnel,
			Service:    fwd.Service,
		}
		go func() {
			if err := f.Serve(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "forward: 127.0.0.1:%d listener stopped: %v\n", fwd.LocalPort, err)
			}
		}()
		fmt.Fprintf(os.Stderr, "forward: 127.0.0.1:%d -> %s:%s\n", fwd.LocalPort, fwd.GatewayLabel, fwd.Service)
	}

	if iface := node.InterfaceName(); iface != "" {
		fmt.Fprintf(os.Stderr, "interface: %s (check with `ifconfig %s` / `netstat -rn | grep %s`)\n", iface, iface, prefix.Masked())
	}
	fmt.Fprintf(os.Stderr, "serving on %s:%d (pubkey %s), tracking %d peer(s), ttl=%s (Ctrl+C to stop)\n", prefix.Addr(), *listenPort, ownPubkey, len(peers), *ttl)
	err = loop.Run(ctx)
	if err == context.Canceled {
		return nil
	}
	return err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
