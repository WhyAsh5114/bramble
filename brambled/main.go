// Package brambled is the per-node mesh daemon: it keeps a WireGuard peer
// table in step with the tailnet registry (admission), finds paths to peers
// (STUN, hole punching, sealed rendezvous, data relays) and enforces service
// ACLs at its gateway. Every check happens at the peer; relays and RPC
// endpoints are untrusted (docs/adr/0003, docs/adr/0019).
package brambled

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/gateway"
	"github.com/WhyAsh5114/bramble/brambled/hederapay"
	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/statusapi"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// Main runs brambled with args (without the program name) and returns an exit code.
// The bramble binary runs it as `bramble node ...`; cmd/brambled wraps it too.
func Main(args []string) int {
	if len(args) < 1 {
		usage()
		return 1
	}
	var err error
	switch args[0] {
	case "resolve":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: brambled resolve <device-label>")
			return 1
		}
		err = runResolve(args[1])
	case "serve":
		err = runServe(args[1:])
	case "genkey":
		var priv, pub string
		if priv, pub, err = wgnode.GenerateKeyPair(); err == nil {
			fmt.Printf("private: %s\n", priv)
			fmt.Printf("public:  %s\n", pub)
		}
	default:
		usage()
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: brambled resolve <device-label>")
	fmt.Fprintln(os.Stderr, "       brambled serve -label <own-label> [-peer label[=allowed-ip/prefix] ...] [-service name=port ...] [-forward local-port=gateway-label:service ...] [-rendezvous host:port@pin] [-netstack]  (omit -peer to track the whole tailnet)")
	fmt.Fprintln(os.Stderr, "       brambled genkey")
}

// chainConfig reads the tailnet and relay directory from the environment:
//
//	BRAMBLE_TAILNET_NAME, BRAMBLE_TAILNET_REGISTRY, BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK
//	BRAMBLE_RELAY_REGISTRY, BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK, BRAMBLE_RELAY_SUFFIX
//	BRAMBLE_RPC_URLS (comma-separated; SEPOLIA_RPC_URL is also read), BRAMBLE_STATE_DIR
//
// The relay directory defaults to the Sepolia one (registry.Sepolia).
func chainConfig() (registry.TailnetConfig, error) {
	cfg := registry.TailnetConfig{
		Name:               os.Getenv("BRAMBLE_TAILNET_NAME"),
		Registry:           os.Getenv("BRAMBLE_TAILNET_REGISTRY"),
		RelayRegistry:      envOr("BRAMBLE_RELAY_REGISTRY", registry.Sepolia.RelayRegistry),
		RelaySuffix:        envOr("BRAMBLE_RELAY_SUFFIX", registry.Sepolia.RelaySuffix),
		RelayRegistryStart: registry.Sepolia.RelayStart,
	}
	if cfg.Name == "" || cfg.Registry == "" {
		return cfg, fmt.Errorf("BRAMBLE_TAILNET_NAME and BRAMBLE_TAILNET_REGISTRY must be set")
	}
	start, err := strconv.ParseUint(os.Getenv("BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK"), 10, 64)
	if err != nil || start == 0 {
		return cfg, fmt.Errorf("BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK must be the registry's deployment block")
	}
	cfg.RegistryStart = start
	if v := os.Getenv("BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK"); v != "" {
		if cfg.RelayRegistryStart, err = strconv.ParseUint(v, 10, 64); err != nil {
			return cfg, fmt.Errorf("BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK: %w", err)
		}
	}
	var urls []string
	for _, u := range strings.Split(os.Getenv("BRAMBLE_RPC_URLS")+","+os.Getenv("SEPOLIA_RPC_URL"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			urls = append(urls, u)
		}
	}
	// User endpoints first, then the public fallbacks.
	cfg.RPC = registry.NewRPC(append(urls, registry.DefaultRPCURLs...))
	if dir := os.Getenv("BRAMBLE_STATE_DIR"); dir != "" {
		cfg.Path = filepath.Join(dir, "registry-cache.json")
	}
	return cfg, nil
}

// startChain builds the registry cache, syncs it once and keeps it syncing until ctx ends.
func startChain(ctx context.Context) (*registry.Tailnet, error) {
	cfg, err := chainConfig()
	if err != nil {
		return nil, err
	}
	tn, err := registry.NewTailnet(cfg)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "registry: syncing %s from the chain...\n", cfg.Name)
	first, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		if err := tn.Sync(first); err == nil {
			break
		} else if first.Err() != nil {
			return nil, fmt.Errorf("first registry sync: %w", err)
		} else {
			fmt.Fprintf(os.Stderr, "registry: sync failed, retrying: %v\n", err)
			time.Sleep(3 * time.Second)
		}
	}
	st := tn.Status()
	fmt.Fprintf(os.Stderr, "registry: synced to block %d\n", st.Head)
	go tn.Run(ctx)
	return tn, nil
}

func runResolve(label string) error {
	tn, err := startChain(context.Background())
	if err != nil {
		return err
	}
	record, err := tn.ResolveDevice(label)
	if err != nil {
		return err
	}
	fmt.Printf("fullname: %s\n", record.Fullname)
	if record.Pubkey != nil {
		fmt.Printf("pubkey:   %s\n", *record.Pubkey)
	} else {
		fmt.Printf("pubkey:   (unset)\n")
	}
	if record.MeshIP != nil {
		fmt.Printf("mesh-ip:  %s\n", *record.MeshIP)
	}
	fmt.Printf("revoked:  %v\n", record.Revoked)
	fmt.Printf("expiry:   %s\n", record.Expiry)
	fmt.Printf("owner:    %s\n", record.Owner)
	return nil
}

// peerFlag accumulates repeated -peer flags: either "label=allowed-ip/prefix"
// (explicit, as before) or a bare "label" (docs/adr/0009) — the latter
// leaves AllowedIP zero, meaning "resolve this peer's mesh-ip from ENS
// instead," filled in by resolvePeerMeshIPs in runServe before the peer
// list is used for anything. Explicit label=ip/prefix always wins outright,
// same precedent as -rendezvous overriding relay discovery.
type peerFlag []admission.Peer

func (p *peerFlag) String() string { return fmt.Sprintf("%v", []admission.Peer(*p)) }

func (p *peerFlag) Set(value string) error {
	label, cidr, ok := strings.Cut(value, "=")
	if !ok {
		*p = append(*p, admission.Peer{Label: value})
		return nil
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return fmt.Errorf("parsing allowed IP for %q: %w", label, err)
	}
	*p = append(*p, admission.Peer{Label: label, AllowedIP: prefix})
	return nil
}

// resolvePeerMeshIPs fills in any peer entry that left AllowedIP zero (the
// bare-label -peer form, docs/adr/0009 — including every peer produced by
// discoverAllPeers, docs/adr/0010) from that device's own mesh-ip ENS
// record, and — if localAddr is empty — resolves this node's own local
// prefix the same way from ownLabel's record. A resolved peer route always
// gets a synthesized /32 regardless of what mask is published on chain, so
// one mis-published mesh-ip can never swallow routing for the whole mesh;
// the local prefix keeps its full on-chain mask, since that's this node's
// own subnet width, not a route to a single peer.
//
// skipUnresolvable changes how a peer that can't be resolved (no usable
// mesh-ip, or a mesh IP colliding with one already claimed) is handled:
// false (an operator explicitly named this peer with -peer) hard-errors,
// since they asked for that specific one and silently dropping it would
// hide a real misconfiguration. true (this peer came from auto-discovery)
// logs a warning and drops just that one peer instead — a live,
// heterogeneous tailnet with one device missing a mesh-ip record shouldn't
// refuse everyone else network access.
func resolvePeerMeshIPs(resolver admission.Resolver, ownLabel, localAddr string, peers []admission.Peer, skipUnresolvable bool) (netip.Prefix, []admission.Peer, error) {
	var localPrefix netip.Prefix
	if localAddr != "" {
		p, err := netip.ParsePrefix(localAddr)
		if err != nil {
			return netip.Prefix{}, nil, fmt.Errorf("parsing -local-addr: %w", err)
		}
		localPrefix = p
	} else {
		p, err := resolveMeshIPPrefix(resolver, ownLabel)
		if err != nil {
			return netip.Prefix{}, nil, fmt.Errorf("no -local-addr given: %w", err)
		}
		localPrefix = p
	}

	used := map[netip.Addr]string{localPrefix.Addr(): ownLabel + " (self)"}
	resolved := make([]admission.Peer, 0, len(peers))
	for _, p := range peers {
		if !p.AllowedIP.IsValid() {
			parsed, err := resolveMeshIPPrefix(resolver, p.Label)
			if err != nil {
				if skipUnresolvable {
					fmt.Fprintf(os.Stderr, "serve: auto-discovered peer %q skipped: %v\n", p.Label, err)
					continue
				}
				return netip.Prefix{}, nil, fmt.Errorf("-peer %q gave no allowed-ip: %w", p.Label, err)
			}
			p.AllowedIP = netip.PrefixFrom(parsed.Addr(), 32)
		}
		addr := p.AllowedIP.Addr()
		if other, ok := used[addr]; ok {
			if skipUnresolvable {
				fmt.Fprintf(os.Stderr, "serve: auto-discovered peer %q skipped: mesh IP collision with %s\n", p.Label, other)
				continue
			}
			return netip.Prefix{}, nil, fmt.Errorf("mesh IP collision: %s and %s both resolve to %s", other, p.Label, addr)
		}
		used[addr] = p.Label
		resolved = append(resolved, p)
	}
	return localPrefix, resolved, nil
}

// discoverAllPeers lists every label ever registered on the tailnet
// (docs/adr/0010) and returns all of them except ownLabel as bare peers,
// for resolvePeerMeshIPs to fill in. Deliberately doesn't pre-filter by
// revoked/expired status — admission.Loop's own per-sync authorization
// check already handles that correctly for a statically-listed peer (it's
// just never admitted), so doing it here too would only be able to get it
// wrong in one direction: excluding a peer that gets un-revoked or
// re-enrolled later, for the life of this process. serve calls this at
// startup and, with auto-discovery on, again every -rediscover interval
// (peerTracker.discoverNew) to pick up devices enrolled after startup.
// deviceLister is satisfied by *sidecar.Manager — kept as a small interface,
// separate from admission.Resolver, so discoverAllPeers can be tested
// without a live sidecar.
type deviceLister interface {
	ListDevices() ([]string, error)
}

func discoverAllPeers(m deviceLister, ownLabel string) ([]admission.Peer, error) {
	labels, err := m.ListDevices()
	if err != nil {
		return nil, fmt.Errorf("auto-discovering peers: %w", err)
	}
	peers := make([]admission.Peer, 0, len(labels))
	for _, label := range labels {
		if label == ownLabel {
			continue
		}
		peers = append(peers, admission.Peer{Label: label})
	}
	return peers, nil
}

// resolveMeshIPPrefix reads label's mesh-ip text record and parses it in
// exactly the format enroll.ts writes and -peer's explicit form already
// accepts ("a.b.c.d/bits") — one parser, one format, on both sides of the
// chain (docs/adr/0009).
func resolveMeshIPPrefix(resolver admission.Resolver, label string) (netip.Prefix, error) {
	record, err := resolver.ResolveDevice(label)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("resolving %q: %w", label, err)
	}
	if record.MeshIP == nil || *record.MeshIP == "" {
		return netip.Prefix{}, fmt.Errorf("%q has no mesh-ip record — set one with enroll.ts or scripts/provision-dev-tailnet/set-mesh-ip.ts, or pass its allowed-ip explicitly", label)
	}
	prefix, err := netip.ParsePrefix(*record.MeshIP)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("parsing %q's on-chain mesh-ip %q: %w", label, *record.MeshIP, err)
	}
	return prefix, nil
}

// serviceFlag accumulates repeated -service name=port flags — this node's
// local, non-ENS service→port config (docs/adr/0005 Q1: never published on
// chain, mapped purely by the receiving gateway).
// A "/open" suffix (name=port/open) opens the service to every admitted
// peer; without it, a peer needs an ACL grant.
type serviceFlag struct {
	ports map[string]uint16
	open  map[string]bool
}

func newServiceFlag() *serviceFlag {
	return &serviceFlag{ports: map[string]uint16{}, open: map[string]bool{}}
}

func (s *serviceFlag) String() string { return fmt.Sprintf("%v open=%v", s.ports, s.open) }

func (s *serviceFlag) Set(value string) error {
	name, portStr, ok := strings.Cut(value, "=")
	if !ok || name == "" || strings.ContainsAny(name, " \n") {
		return fmt.Errorf("expected name=port or name=port/open, got %q", value)
	}
	portStr, open := strings.CutSuffix(portStr, "/open")
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port == 0 {
		return fmt.Errorf("parsing port for service %q: %v", name, err)
	}
	s.ports[name] = uint16(port)
	if open {
		s.open[name] = true
	}
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

func runServe(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Serve(ctx, nil, args, nil)
}

// Serve runs a node until ctx ends. tn is the registry cache to use; nil
// starts one from the environment (chainConfig). onReady, if set, receives a
// Handle once the node is up. The bramble daemon embeds the node this way.
func Serve(ctx context.Context, tn *registry.Tailnet, args []string, onReady func(*Handle)) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var peers peerFlag
	fs.Var(&peers, "peer", "peer to track: label=allowed-ip/prefix, or a bare label to resolve that peer's mesh-ip from ENS instead (repeatable, docs/adr/0009) — requires the label to have a mesh-ip record, written by enroll.ts. Omit entirely to auto-discover and track every other device on the tailnet instead (docs/adr/0010); any -peer given at all disables auto-discovery and uses exactly what was typed")
	services := newServiceFlag()
	fs.Var(services, "service", "local service this node offers as a gateway: name=local-port (repeatable); omit to offer none (docs/adr/0006-gateway-connect-protocol.md)")
	var forwards forwardFlag
	fs.Var(&forwards, "forward", "local port-forward to a peer's gateway service: local-port=gateway-label:service (repeatable) — lets an unmodified local client (psql, curl, ...) reach an ACL'd mesh service with no CONNECT knowledge of its own (docs/adr/0006-gateway-connect-protocol.md)")
	localPrefix := fs.String("local-addr", "", "this node's address and mesh subnet (CIDR); omit to resolve it from -label's own on-chain mesh-ip record instead (docs/adr/0009) — explicit value always wins")
	stunServers := fs.String("stun-servers", os.Getenv("BRAMBLE_STUN_SERVERS"), "comma-separated STUN host:port servers; empty uses the chosen relays' STUN servers")
	noSTUN := fs.Bool("no-stun", false, "turn off public endpoint discovery and hole punching")
	listenPort := fs.Uint("listen-port", 51820, "UDP port this node's WireGuard transport binds to")
	ttl := fs.Duration("ttl", 30*time.Second, "how often to re-resolve ENS state for tracked peers")
	rediscover := fs.Duration("rediscover", defaultRediscoverInterval, "with auto-discovery (no -peer flags), how often to re-list the tailnet and start tracking devices enrolled after startup, without a restart (docs/adr/0010); 0 turns rediscovery off. Admission and ACL checks for a new device are unchanged")
	maxStale := fs.Duration("max-stale", 2*time.Minute, "remove a peer if resolving it keeps failing for this long (sidecar/RPC down) instead of leaving it admitted indefinitely; defaults to a bounded 2 minutes rather than unbounded fail-open — pass 0 explicitly to disable this and fail open forever (see admission package doc)")
	keepaliveSeconds := fs.Int("keepalive", 0, "send an unsolicited WireGuard packet to every authorized peer this often (seconds) so NAT mappings/roaming state don't go stale on an otherwise-idle connection; 0 disables this (existing behavior — a data-relay pool already arms its own keepalive independently of this flag, see dataRelayKeepaliveSeconds)")
	privateKeyHex := fs.String("private-key", os.Getenv("BRAMBLE_PRIVATE_KEY"), "hex-encoded WireGuard private key (generated ephemerally if unset — not persisted)")
	ownLabel := fs.String("label", "", "this node's own ENS label — required to resolve its own acl-granters record for gateway enforcement (docs/adr/0005-acl-record-schema.md)")
	gatewayPort := fs.Uint("gateway-port", 7892, "fixed TCP port this node's CONNECT gateway listener binds to on its own tunnel address (7892, not 7891 — that's relay-sidecar's default port, and both can end up on the same machine during local dev/testing, docs/adr/0008)")
	statusPort := fs.Uint("status-port", 7899, "port for this node's own status API (live WireGuard peer state, ping, recent admission/gateway activity) — read-only diagnostics for local tooling like the dashboard, see brambled/statusapi")
	statusBind := fs.String("status-bind", "127.0.0.1", "bind address for the status API — loopback-only by default; set to 0.0.0.0 only if a dashboard on a different machine genuinely needs to reach this node's status API remotely")
	interfaceName := fs.String("interface", "", "real OS interface name (auto-picked if unset: \"utun\" on macOS, \"bramble0\" on linux)")
	netstackMode := fs.Bool("netstack", false, "use a virtual (gVisor) TUN instead of a real OS interface — testing/local dev only, never the demo")
	rendezvousAddr := fs.String("rendezvous", "", "rendezvous relay as host:port@pin; omit to use the relay directory")
	myCandidate := fs.String("my-candidate", "", "this node's own reachable address (ip:port) to publish via the rendezvous relay, e.g. a VPS's public IP:listen-port")
	dataRelays := dataRelayFlag{}
	fs.Var(dataRelays, "data-relay", "data relay as label=host:port@pin (repeatable); omit to use the relay directory")
	relayPeers := relayPeerFlag{}
	fs.Var(relayPeers, "relay-peer", "tracked -peer label that should use the data-relay pool instead of a direct candidate (repeatable) — forces relay routing even when STUN traversal is enabled (docs/adr/0008)")
	relaySessionBytes := fs.Int64("relay-session-bytes", 256<<20, "bytes to ask for per data-relay session; the relay may grant less")
	hederaAccount := fs.String("hedera-account", os.Getenv("BRAMBLE_HEDERA_ACCOUNT"), "this node's own Hedera testnet account (0.0.N); with -hedera-key it turns on paying relays that charge for data sessions over x402 (docs/adr/0021). Off when unset")
	hederaKey := fs.String("hedera-key", os.Getenv("BRAMBLE_HEDERA_KEY"), "hex ECDSA private key of -hedera-account. It cannot write tailnet records and is never printed or logged")
	payMaxSession := fs.Int64("pay-max-session", defaultMaxSession, "most one paid data session may cost, in USDC's smallest unit (six decimals)")
	payMaxDay := fs.Int64("pay-max-day", defaultMaxDay, "most this node pays in a UTC day, in USDC's smallest unit")
	relayFirst := fs.Bool("relay-first", false, "route every tracked peer through the data-relay pool instead of a direct path, for a device with no direct path to the mesh")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// No explicit -peer requirement anymore (docs/adr/0010) — an empty
	// list means "auto-discover every other device on the tailnet"
	// instead of an error; see the discoverAllPeers call below, after the
	// sidecar exists to ask.
	if *relaySessionBytes <= 0 {
		return fmt.Errorf("-relay-session-bytes must be positive")
	}
	if *ownLabel == "" {
		return fmt.Errorf("-label (this node's own ENS label) is required")
	}
	// The len(dataRelays)==0 case is checked further below, after relay
	// discovery has had a chance to populate the pool — -data-relay isn't
	// the only source of it anymore (docs/adr/0003's Consequence section).

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

	if _, err := rendezvous.RegisterIdentity(*privateKeyHex); err != nil {
		return fmt.Errorf("registering rendezvous identity: %w", err)
	}

	m := tn
	if m == nil {
		if m, err = startChain(ctx); err != nil {
			return err
		}
	}
	var sessions sessionSource = freeSessions{}
	paying := *hederaAccount != "" && *hederaKey != ""
	var paySource *paidSessions
	if paying {
		signer, err := hederapay.NewSigner(*hederaAccount, *hederaKey)
		if err != nil {
			return err
		}
		paySource = newPaidSessions(signer, *payMaxSession, *payMaxDay)
		sessions = paySource
		fmt.Fprintf(os.Stderr, "payments on: %s pays relays that charge, up to %d per session and %d per day (testnet USDC)\n", signer.Account(), *payMaxSession, *payMaxDay)
	}

	// No -peer flags at all: auto-discover every other device on the
	// tailnet instead of requiring each one typed out by hand
	// (docs/adr/0010) — full-mesh-by-default, matching what a real
	// tailnet gives you elsewhere. Any -peer flags given at all disable
	// this entirely and use exactly what was typed, unchanged.
	autoDiscovering := len(peers) == 0
	if autoDiscovering {
		discovered, discErr := discoverAllPeers(m, *ownLabel)
		if discErr != nil {
			return fmt.Errorf("no -peer flags given: %w", discErr)
		}
		peers = discovered
		fmt.Fprintf(os.Stderr, "no -peer flags given — auto-discovered %d other device(s) on the tailnet\n", len(peers))
	}

	// Fills in any -peer that gave a bare label (docs/adr/0009) from that
	// device's own on-chain mesh-ip, and resolves this node's own local
	// prefix the same way if -local-addr was left unset. Must run before
	// peerAddrByLabel below, and before anything that dials a peer address.
	// Auto-discovered peers that can't resolve a usable mesh-ip are
	// skipped with a warning rather than aborting the whole node — an
	// operator who explicitly named a peer gets a hard error instead,
	// since they asked for that specific one.
	prefix, resolvedPeers, err := resolvePeerMeshIPs(m, *ownLabel, *localPrefix, peers, autoDiscovering)
	if err != nil {
		return err
	}
	peers = resolvedPeers
	if len(peers) == 0 {
		fmt.Fprintln(os.Stderr, "warning: tracking zero peers — nothing to admit or connect to")
	}

	trackedPeerLabels := make(map[string]bool, len(peers))
	for _, p := range peers {
		trackedPeerLabels[p.Label] = true
	}
	for _, fwd := range forwards {
		if !trackedPeerLabels[fwd.GatewayLabel] {
			return fmt.Errorf("-forward %d=%s:%s: %q is not a tracked -peer — a forward target must be a peer this node admits", fwd.LocalPort, fwd.GatewayLabel, fwd.Service, fwd.GatewayLabel)
		}
	}
	if *relayFirst {
		for label := range trackedPeerLabels {
			relayPeers[label] = true
		}
	}
	for label := range relayPeers {
		if !trackedPeerLabels[label] {
			return fmt.Errorf("-relay-peer %q is not a tracked -peer", label)
		}
	}

	peerAddrByLabel := make(map[string]netip.Addr, len(peers))
	for _, p := range peers {
		peerAddrByLabel[p.Label] = p.AllowedIP.Addr()
	}

	// Relay discovery (docs/adr/0003, docs/adr/0020). -rendezvous,
	// -data-relay and -stun-servers/-no-stun override the directory for their
	// own set; a set left unset comes from the directory and is refreshed
	// while the node runs, so a relay registered after startup is picked up
	// without a restart. An unreachable or empty directory is not fatal.
	usesDirectory := *rendezvousAddr == "" || len(dataRelays) == 0
	relays := newRelayDirectory(func() ([]rendezvous.Relay, dataRelayFlag, []stunServer, error) {
		dir, err := m.Relays()
		if err != nil {
			return nil, nil, nil, err
		}
		rdv, data, stun := chooseRelays(dir, m.PreferredRelays(), paying)
		return rdv, data, stun, nil
	}, relaySets{
		Rendezvous: staticRendezvousRelays(*rendezvousAddr), FixedRendezvous: *rendezvousAddr != "",
		Data: dataRelays, FixedData: len(dataRelays) > 0,
		STUN: splitServers(*stunServers), FixedSTUN: *noSTUN || *stunServers != "" || !usesDirectory,
	})
	if usesDirectory {
		if relErr := relays.refresh(); relErr != nil {
			fmt.Fprintf(os.Stderr, "relay discovery: %v (will retry)\n", relErr)
		}
		rdv, _ := relays.Rendezvous()
		tracef("relays: rendezvous %v, data %v, stun %v", rdv, relayAddresses(relays.DataRelays()), relays.STUN())
	}
	if len(relayPeers) > 0 && len(relays.DataRelays()) == 0 {
		return fmt.Errorf("-relay-peer requires at least one data relay, from -data-relay or discovery")
	}

	// STUN is on unless -no-stun is set or every relay set came from flags
	// with no -stun-servers. It is decided once, from flags, so a peer never
	// switches between traversal and the static resolver mid-run; the server
	// list itself is read live by RunSTUN.
	stunEnabled := !*noSTUN && (*stunServers != "" || usesDirectory)
	node, err := wgnode.New(wgnode.Config{
		EnableSTUN:    stunEnabled,
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
	status := &statusapi.Server{PeerTable: node, EndpointSource: node, DataRelays: func() map[string]string { return relayAddresses(relays.DataRelays()) }}

	tracker := newPeerTracker(*ownLabel, prefix.Addr(), peers)
	useTraversal := func(p admission.Peer) bool {
		return stunEnabled && p.Endpoint == nil && !relayPeers[p.Label]
	}
	paths := &peerTraversal{
		ctx: ctx, node: node, own: ownPubkey, relaySet: nil, sessions: sessions, pool: relays.DataRelays,
		sessionBytes: *relaySessionBytes, exchangeTimeout: *ttl,
		eligible: func(label string) bool {
			tracker.mu.Lock()
			defer tracker.mu.Unlock()
			p, ok := tracker.byLabel[label]
			return ok && useTraversal(p)
		},
		local: func() rendezvous.Announcement {
			var local rendezvous.Announcement
			if ep, err := netip.ParseAddrPort(*myCandidate); err == nil {
				local = append(local, rendezvous.Endpoint{Kind: rendezvous.KindStatic, Addr: ep})
			}
			if observed := node.EndpointStatus(); observed.State == "discovered" {
				if ep, err := netip.ParseAddrPort(observed.Endpoint); err == nil {
					local = append(local, rendezvous.Endpoint{Kind: rendezvous.KindSTUN, Addr: ep})
				}
			}
			return local
		},
		onState: status.OnPathEvent,
	}

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
			if e.Authorized && relayPeers[e.Label] {
				status.OnPathEvent(e.Label, e.PublicKey, "relay")
			}
			paths.Handle(e)
		},
	}

	// relaySet is what every consumer of the rendezvous set reads: the
	// resolver below, traversal and both data-relay watchers. It reads the
	// live directory, so a relay found after startup reaches all of them.
	relaySet := rendezvousSource(relays.Rendezvous)
	paths.relaySet = relaySet
	{
		backoff := newExchangeBackoff(*ttl, exchangeBackoffMax)
		own, defaultCandidate := ownPubkey, *myCandidate
		initialSessions := make(map[string]struct {
			pubkey  string
			session *dataRelaySession
		})
		resolve := func(rdv []rendezvous.Relay, p admission.Peer, peerPubkey string) (*netip.AddrPort, error) {
			if relayPeers[p.Label] {
				cached := initialSessions[p.Label]
				if cached.pubkey != peerPubkey {
					cached.session = nil
				}
				endpoint, session, err := relayRoutedEndpoint(node, sessions, relays.DataRelays(), *relaySessionBytes, rdv, own, peerPubkey, *ttl, cached.session)
				cached.pubkey, cached.session = peerPubkey, session
				initialSessions[p.Label] = cached
				if err == nil {
					relState.set(p.Label, peerPubkey, session)
				}
				return endpoint, err
			}
			// Recorded even for non-relay peers: watchDataRelayAdopt below
			// needs a peerPubkey to watch, and EndpointResolver is the only
			// place that ever learns one.
			relState.setPubkey(p.Label, peerPubkey)

			candidate, _, err := exchangeAny(rdv, own, peerPubkey, defaultCandidate, *ttl)
			if err != nil {
				return nil, err
			}
			if candidate == "" {
				return nil, nil // peer published no reachable address of its own
			}
			endpoint, err := peerEndpointFromCandidate(candidate)
			if err != nil {
				return nil, fmt.Errorf("peer %s published an unusable candidate: %w", short(peerPubkey), err)
			}
			adoptBind(node, candidate, endpoint)
			return &endpoint, nil
		}
		loop.EndpointResolver = func(p admission.Peer, peerPubkey string) (*netip.AddrPort, error) {
			if useTraversal(p) {
				return nil, nil
			}
			rdv, err := relaySet()
			if err != nil {
				return nil, err
			}
			// A peer that failed to answer recently is skipped: each attempt
			// blocks this loop for a whole exchange timeout.
			if wait := backoff.Wait(peerPubkey); wait > 0 {
				return nil, fmt.Errorf("skipping rendezvous with %s for %s: it did not answer the last attempt", p.Label, wait.Round(time.Second))
			}
			endpoint, err := resolve(rdv, p, peerPubkey)
			if err != nil {
				backoff.Fail(peerPubkey)
			} else {
				backoff.Succeed(peerPubkey)
			}
			return endpoint, err
		}
	}

	if stunEnabled {
		go node.RunSTUN(ctx, relays.STUN, status.OnEndpointEvent)
	}
	if usesDirectory {
		go relays.run(ctx, relayRefreshInterval, os.Stderr)
	}

	// tracker is the live tracked set the gateway maps tunnel addresses
	// with. Rediscovery (below) adds to it; startup peers are in it now.

	// Gate 4.3 failover (docs/adr/0008): one watcher per -relay-peer,
	// started once ctx exists so Ctrl+C stops them cleanly. Each blocks
	// until relState reports an initial connect, then waits for a stall.
	for _, p := range peers {
		if !relayPeers[p.Label] {
			continue
		}
		p := p
		go watchDataRelayFailover(ctx, node, sessions, relState, p.Label, relays.DataRelays, *relaySessionBytes, relaySet, ownPubkey, *ttl)
	}

	// The other half of Gate 4.3: every tracked peer that ISN'T a
	// -relay-peer on this node still needs a way to notice if *it's* the
	// one relay-routed on the other end and just failed over — see
	// watchDataRelayAdopt's doc comment. Scoped to deployments that opted
	// into data-relay routing at all (a data-relay pool + rendezvous set),
	// so a plain direct-peer setup with no relay involved anywhere keeps
	// its old behavior exactly (no extra re-exchange traffic on an
	// ordinary idle connection). The pool is read live: a watcher waits
	// until discovery has found a data relay.
	startAdoptWatcher := func(p admission.Peer) {
		if relayPeers[p.Label] || useTraversal(p) {
			return
		}
		if !usesDirectory && len(relays.DataRelays()) == 0 {
			return // relays all come from flags and there is no data relay: nothing to adopt, ever
		}
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				if len(relays.DataRelays()) == 0 {
					continue
				}
				if _, err := relaySet(); err != nil {
					continue
				}
				pubkey, ok := relState.getPubkey(p.Label)
				if !ok {
					continue
				}
				watchDataRelayAdopt(ctx, node, p.Label, ownPubkey, pubkey, *myCandidate, p.AllowedIP, relaySet)
				return
			}
		}()
	}
	for _, p := range peers {
		startAdoptWatcher(p)
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
		Services:      services.ports,
		Open:          services.open,
		LookupPeer:    tracker.lookup,
		OnDecision:    status.OnGatewayDecision,
	}
	go func() {
		if err := gw.Serve(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "gateway: listener stopped: %v\n", err)
		}
	}()
	fmt.Fprintf(os.Stderr, "gateway: listening on %s:%d, offering %d service(s)\n", prefix.Addr(), *gatewayPort, len(services.ports))

	if *statusPort != 0 {
		go func() {
			if err := status.ListenAndServe(ctx, *statusBind, *statusPort); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "status: listener stopped: %v\n", err)
			}
		}()
		fmt.Fprintf(os.Stderr, "status: listening on %s:%d\n", *statusBind, *statusPort)
	}

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
	// Rediscovery (docs/adr/0010): only with auto-discovery. An explicit
	// -peer list stays exactly what was typed. A new peer goes through the
	// same admission.Loop checks and gateway ACL as a startup peer.
	if autoDiscovering && *rediscover > 0 {
		go rediscoverPeers(ctx, *rediscover, tracker, m, m, os.Stderr, func(added []admission.Peer) {
			for _, p := range loop.AddPeers(added) {
				fmt.Fprintf(os.Stderr, "serve: rediscovered peer %s (%s), now tracking it\n", p.Label, p.AllowedIP)
				startAdoptWatcher(p)
			}
		})
	}

	fmt.Fprintf(os.Stderr, "serving on %s:%d (pubkey %s), tracking %d peer(s), ttl=%s, rediscover=%s (Ctrl+C to stop)\n", prefix.Addr(), *listenPort, ownPubkey, len(peers), *ttl, *rediscover)
	if onReady != nil {
		onReady(&Handle{pay: paySource, payAccount: *hederaAccount, node: node, tracker: tracker, status: status, gatewayPort: uint16(*gatewayPort), netstack: *netstackMode, Label: *ownLabel, Address: prefix.Addr(), PublicKey: ownPubkey})
	}
	err = loop.Run(ctx)
	if err == context.Canceled || ctx.Err() != nil {
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

// splitServers splits a comma-separated host:port list, dropping empty entries.
func splitServers(list string) []string {
	var out []string
	for _, server := range strings.Split(list, ",") {
		if server = strings.TrimSpace(server); server != "" {
			out = append(out, server)
		}
	}
	return out
}

// relayAddresses reports the data-relay pool as label -> rendezvous address for the status API.
func relayAddresses(pool dataRelayFlag) map[string]string {
	out := make(map[string]string, len(pool))
	for label, r := range pool {
		out[label] = r.Address
	}
	return out
}
