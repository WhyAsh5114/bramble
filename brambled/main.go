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
// implemented — see docs/11_DAY0_GATES.md Gate 0.2's note on laptop-to-VPS
// being the easier, currently-supported topology.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/sidecar"
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
	fmt.Fprintln(os.Stderr, "       brambled serve [-peer label=allowed-ip/prefix ...] [-rendezvous host:port] [-netstack]")
	fmt.Fprintln(os.Stderr, "       brambled genkey")
}

func sidecarConfig() sidecar.Config {
	return sidecar.Config{
		Dir:             envOr("BRAMBLE_SIDECAR_DIR", "../sidecar"),
		Port:            7890,
		TailnetName:     os.Getenv("BRAMBLE_TAILNET_NAME"),
		TailnetRegistry: os.Getenv("BRAMBLE_TAILNET_REGISTRY"),
		RPCURL:          os.Getenv("SEPOLIA_RPC_URL"),
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

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var peers peerFlag
	fs.Var(&peers, "peer", "peer to track: label=allowed-ip/prefix (repeatable)")
	localPrefix := fs.String("local-addr", "10.77.0.1/24", "this node's address and mesh subnet (CIDR)")
	listenPort := fs.Uint("listen-port", 51820, "UDP port this node's WireGuard transport binds to")
	ttl := fs.Duration("ttl", 30*time.Second, "how often to re-resolve ENS state for tracked peers")
	privateKeyHex := fs.String("private-key", os.Getenv("BRAMBLE_PRIVATE_KEY"), "hex-encoded WireGuard private key (generated ephemerally if unset — not persisted)")
	interfaceName := fs.String("interface", "", "real OS interface name (auto-picked if unset: \"utun\" on macOS, \"bramble0\" on linux)")
	netstackMode := fs.Bool("netstack", false, "use a virtual (gVisor) TUN instead of a real OS interface — testing/local dev only, never the demo")
	rendezvousAddr := fs.String("rendezvous", "", "rendezvous relay address (host:port) for candidate exchange; omit for static/dial-in-first peers only")
	myCandidate := fs.String("my-candidate", "", "this node's own reachable address (ip:port) to publish via the rendezvous relay, e.g. a VPS's public IP:listen-port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(peers) == 0 {
		return fmt.Errorf("at least one -peer label=allowed-ip/prefix is required")
	}

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

	loop := &admission.Loop{
		Resolver: m,
		Table:    node,
		Peers:    peers,
		TTL:      *ttl,
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
		},
	}

	if *rendezvousAddr != "" {
		relayAddr, own, ownCandidate := *rendezvousAddr, ownPubkey, *myCandidate
		loop.EndpointResolver = func(_ admission.Peer, peerPubkey string) (*netip.AddrPort, error) {
			candidate, err := rendezvous.Exchange(relayAddr, own, peerPubkey, ownCandidate, *ttl)
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
			return &endpoint, nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
