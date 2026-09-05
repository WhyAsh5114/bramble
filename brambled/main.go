// brambled is the per-node mesh networking daemon.
//
// Phase 1 Section A: spawn its own local ENS sidecar (docs/adr/0002) and
// resolve a real device subname through it (`resolve`).
// Phase 1 Section B: the admission verifier (Gate 1.1) — keep a WireGuard
// device's peer table synced to ENS state (`serve`). See
// docs/05_BUILD_PLAN.md Phase 1 and the Section B plan for what's next
// (Gate 1.2 rendezvous/hole-punching is not implemented yet — `serve` only
// manages the peer table, it does not attempt cross-machine connectivity).
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
	fmt.Fprintln(os.Stderr, "       brambled serve [-peer label=allowed-ip/prefix ...]")
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
	localAddr := fs.String("local-addr", "10.77.0.1", "this node's virtual address on its own WireGuard interface")
	listenPort := fs.Uint("listen-port", 51820, "UDP port this node's WireGuard transport binds to")
	ttl := fs.Duration("ttl", 30*time.Second, "how often to re-resolve ENS state for tracked peers")
	privateKeyHex := fs.String("private-key", os.Getenv("BRAMBLE_PRIVATE_KEY"), "hex-encoded WireGuard private key (generated ephemerally if unset — not persisted)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(peers) == 0 {
		return fmt.Errorf("at least one -peer label=allowed-ip/prefix is required")
	}

	addr, err := netip.ParseAddr(*localAddr)
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

	node, err := wgnode.New(wgnode.Config{PrivateKeyHex: *privateKeyHex, ListenPort: uint16(*listenPort), LocalAddress: addr})
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
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "serving on %s:%d, tracking %d peer(s), ttl=%s (Ctrl+C to stop)\n", addr, *listenPort, len(peers), *ttl)
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
