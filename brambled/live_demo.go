// Live-demo driver for Phase 4's data-plane relay (docs/adr/0008,
// docs/05_BUILD_PLAN.md Gates 4.2/4.3): proves cost-scales-with-bytes and
// selection+kill-mid-transfer failover against the real relay binary, real
// relay-sidecar, and real Hedera testnet settlement — not the fakes
// datarelay_endpoint_test.go uses. It calls the exact same unexported
// functions runServe's EndpointResolver does (buyDataRelaySession,
// relayRoutedEndpoint, watchDataRelayFailover), so this is a driver, not a
// reimplementation. Not part of serve's own startup path — invoked
// explicitly via `brambled demo-data-relay`.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// hashscanTestnetTxURL turns a Hedera consensus transaction ID (the
// "0.0.X@seconds.nanos" form the sidecar already returns, not the mirror
// node's dash form dashboard/src/app/api/hedera/payments/route.ts converts
// from) into a clickable HashScan testnet link — Luke Forrest's own advice
// (docs/sources/hedera_workshop_clean.md) is that judges check this, so
// the demo output should make it one click away, not just printable text.
func hashscanTestnetTxURL(txID string) string {
	if txID == "" {
		return ""
	}
	return "https://hashscan.io/testnet/transaction/" + txID
}

// resolveDemoRelayEndpoints tries the ENS relay directory first (real
// on-chain discovery via the sidecar's /relays route, which mirrors
// sidecar/src/ens/relays.ts's resolveRelays) and falls back to the
// caller's static -primary/-backup/-rendezvous/-rendezvous-backup flags
// whenever ENS doesn't publish at least two priced data relays with a
// matching rendezvous-address record each. Same fallback contract
// sidecar/client.go's Relays() already documents ("callers should fall
// back to their own static overrides") — this is that fallback, made
// explicit here instead of silently trusting ENS mid-recording.
func resolveDemoRelayEndpoints(m *sidecar.Manager, fallbackPrimaryURL, fallbackBackupURL, fallbackRendezvous, fallbackRendezvousBackup string) (primaryURL, backupURL, rendezvousAddr, rendezvousBackupAddr string, viaENS bool) {
	resp, err := m.Relays()
	if err != nil {
		fmt.Fprintf(os.Stderr, "relays-from-ens: lookup failed (%v) -- falling back to static flags\n", err)
		return fallbackPrimaryURL, fallbackBackupURL, fallbackRendezvous, fallbackRendezvousBackup, false
	}
	if len(resp.DataRelays) < 2 {
		fmt.Fprintf(os.Stderr, "relays-from-ens: found %d priced data relay(s), need 2 -- falling back to static flags\n", len(resp.DataRelays))
		return fallbackPrimaryURL, fallbackBackupURL, fallbackRendezvous, fallbackRendezvousBackup, false
	}

	rendezvousByLabel := make(map[string]string, len(resp.Rendezvous))
	for _, r := range resp.Rendezvous {
		rendezvousByLabel[r.Label] = r.Address
	}

	relays := append([]sidecar.DataRelay(nil), resp.DataRelays...)
	sort.Slice(relays, func(i, j int) bool {
		pi, erri := strconv.ParseInt(relays[i].PricePerByte, 10, 64)
		pj, errj := strconv.ParseInt(relays[j].PricePerByte, 10, 64)
		if erri != nil || errj != nil {
			return false
		}
		return pi < pj
	})
	cheap, expensive := relays[0], relays[1]
	cheapRendezvous, cheapOK := rendezvousByLabel[cheap.Label]
	expensiveRendezvous, expensiveOK := rendezvousByLabel[expensive.Label]
	if !cheapOK || !expensiveOK {
		fmt.Fprintln(os.Stderr, "relays-from-ens: a priced data relay published no matching rendezvous-address record -- falling back to static flags")
		return fallbackPrimaryURL, fallbackBackupURL, fallbackRendezvous, fallbackRendezvousBackup, false
	}

	fmt.Fprintf(os.Stderr, "relays-from-ens: %s quotes %s atomic/byte (primary), %s quotes %s atomic/byte (backup)\n",
		cheap.Label, cheap.PricePerByte, expensive.Label, expensive.PricePerByte)
	return cheap.SidecarURL, expensive.SidecarURL, cheapRendezvous, expensiveRendezvous, true
}

func runDemoDataRelay(args []string) error {
	fs := flag.NewFlagSet("demo-data-relay", flag.ContinueOnError)
	rendezvousAddr := fs.String("rendezvous", "127.0.0.1:9420", "primary relay's rendezvous TCP address")
	rendezvousBackupAddr := fs.String("rendezvous-backup", "127.0.0.1:9421", "backup relay's rendezvous TCP address — killing the primary process takes its rendezvous listener down too, so failover's own candidate re-exchange needs somewhere else to go (docs/adr/0003's exchangeAnyMetered)")
	primaryURL := fs.String("primary", "http://127.0.0.1:7891", "primary (cheap) relay-sidecar URL")
	backupURL := fs.String("backup", "http://127.0.0.1:7893", "backup (expensive) relay-sidecar URL")
	smallBytes := fs.Int64("small-bytes", 1000, "small session size, for the cost-scaling proof")
	largeBytes := fs.Int64("large-bytes", 100000, "large session size, for the cost-scaling proof")
	sessionBytes := fs.Int64("session-bytes", 5000, "session size used for the selection+failover run")
	maxPricePerByte := fs.Int64("max-price-per-byte", 1000, "maximum relay price accepted during this deliberately high-price comparison")
	maxSessionCost := fs.Int64("max-session-cost", 10_000_000, "maximum session cost in atomic USDC for this demo; also set HEDERA_MAX_PAYMENT_ATOMIC to at least this value")
	sidecarDir := fs.String("sidecar-dir", "../sidecar", "brambled's own ENS sidecar project directory")
	relaysFromENS := fs.Bool("relays-from-ens", false, "resolve both relays' rendezvous address, sidecar URL, and price from the ENS relay directory instead of -primary/-backup/-rendezvous/-rendezvous-backup; falls back to those flags if ENS doesn't publish at least two priced data relays")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *smallBytes <= 0 || *largeBytes <= 0 || *sessionBytes <= 0 || *maxPricePerByte <= 0 || *maxSessionCost <= 0 {
		return fmt.Errorf("session sizes and relay price/cost limits must all be positive")
	}

	hederaAccount := envOr("HEDERA_CLIENT_ACCOUNT_ID", "")
	hederaKey := envOr("HEDERA_CLIENT_PRIVATE_KEY", "")
	tailnetName := envOr("BRAMBLE_TAILNET_NAME", "")
	tailnetRegistry := envOr("BRAMBLE_TAILNET_REGISTRY", "")
	if hederaAccount == "" || hederaKey == "" || tailnetName == "" || tailnetRegistry == "" {
		return fmt.Errorf("HEDERA_CLIENT_ACCOUNT_ID, HEDERA_CLIENT_PRIVATE_KEY, BRAMBLE_TAILNET_NAME, and BRAMBLE_TAILNET_REGISTRY must all be set")
	}
	policy := dataRelayPurchasePolicy{MaxPricePerByte: *maxPricePerByte, MaxSessionCost: *maxSessionCost}

	newManager := func(port int) (*sidecar.Manager, error) {
		return sidecar.Start(sidecar.Config{
			Dir:                    *sidecarDir,
			Port:                   port,
			TailnetName:            tailnetName,
			TailnetRegistry:        tailnetRegistry,
			HederaClientAccountID:  hederaAccount,
			HederaClientPrivateKey: hederaKey,
		})
	}

	fmt.Fprintln(os.Stderr, "=== Gate 4.2: cost scales with bytes (one relay, one price, two sizes) ===")
	scaleM, err := newManager(7999)
	if err != nil {
		return fmt.Errorf("starting sidecar for cost-scaling run: %w", err)
	}
	defer scaleM.Stop()

	primaryEndpoint, backupEndpoint, rendezvousPrimary, rendezvousBackup := *primaryURL, *backupURL, *rendezvousAddr, *rendezvousBackupAddr
	if *relaysFromENS {
		primaryEndpoint, backupEndpoint, rendezvousPrimary, rendezvousBackup, _ = resolveDemoRelayEndpoints(scaleM, *primaryURL, *backupURL, *rendezvousAddr, *rendezvousBackupAddr)
	}

	small, err := buyDataRelaySession(scaleM, dataRelayFlag{"primary": primaryEndpoint}, *smallBytes, policy)
	if err != nil {
		return fmt.Errorf("buying small session: %w", err)
	}
	fmt.Fprintf(os.Stderr, "small session (%d bytes): id=%s tx=%s\n", *smallBytes, small.id, small.settlementTxID)
	if u := hashscanTestnetTxURL(small.settlementTxID); u != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", u)
	}

	large, err := buyDataRelaySession(scaleM, dataRelayFlag{"primary": primaryEndpoint}, *largeBytes, policy)
	if err != nil {
		return fmt.Errorf("buying large session: %w", err)
	}
	fmt.Fprintf(os.Stderr, "large session (%d bytes): id=%s tx=%s\n", *largeBytes, large.id, large.settlementTxID)
	if u := hashscanTestnetTxURL(large.settlementTxID); u != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", u)
	}

	fmt.Fprintln(os.Stderr, "=== Gate 4.3: selection + kill-mid-transfer failover (two relays, real prices) ===")
	aliceM, err := newManager(8000)
	if err != nil {
		return fmt.Errorf("starting alice's sidecar: %w", err)
	}
	defer aliceM.Stop()
	bobM, err := newManager(8001)
	if err != nil {
		return fmt.Errorf("starting bob's sidecar: %w", err)
	}
	defer bobM.Stop()

	alicePriv, alicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generating alice's keypair: %w", err)
	}
	bobPriv, bobPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generating bob's keypair: %w", err)
	}

	aliceAddr := netip.MustParseAddr("10.99.0.1")
	bobAddr := netip.MustParseAddr("10.99.0.2")
	aliceIP := netip.PrefixFrom(aliceAddr, 32)
	bobIP := netip.PrefixFrom(bobAddr, 32)

	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: 61970, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		return fmt.Errorf("starting alice's wgnode: %w", err)
	}
	defer alice.Close()
	bob, err := wgnode.New(wgnode.Config{PrivateKeyHex: bobPriv, ListenPort: 61971, LocalAddress: bobAddr, Netstack: true})
	if err != nil {
		return fmt.Errorf("starting bob's wgnode: %w", err)
	}
	defer bob.Close()

	pool := dataRelayFlag{"primary": primaryEndpoint, "backup": backupEndpoint}
	// Each relay paired with its OWN payment sidecar — not just a flat list
	// of TCP addresses. This pairing is the fix docs/adr/0008 records:
	// every relay mints rendezvous tokens under its own process-local
	// secret, so a token bought from the primary's sidecar was never valid
	// at the backup's TCP listener once the primary died mid-failover.
	// exchangeAnyMetered (via relayRoutedEndpoint/watchDataRelayFailover/
	// watchDataRelayAdopt) now buys a fresh token from whichever relay it's
	// actually about to try.
	relays := []sidecar.RendezvousRelay{
		{Label: "primary", Address: rendezvousPrimary, SidecarURL: primaryEndpoint},
		{Label: "backup", Address: rendezvousBackup, SidecarURL: backupEndpoint},
	}

	// Asymmetric, matching docs/adr/0008 and runServe's actual wiring:
	// only alice is -relay-peer for bob (she buys, publishes, and later
	// watches for a stall to fail over from). Bob never buys a session —
	// he just adopts whatever candidate alice publishes, the same as any
	// ordinary peer using the plain rendezvous path. A real relay hands
	// out a fresh port per session bought, so having both sides
	// independently buy (an earlier draft of this demo) only ever
	// "worked" by accident against a stub that didn't do that — a live
	// run against the real relay binary is what surfaced it.
	var aliceEndpoint *netip.AddrPort
	var aliceLabel string
	var aliceErr error
	var bobEndpoint *netip.AddrPort
	var bobErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceEndpoint, aliceLabel, aliceErr = relayRoutedEndpoint(alice, aliceM, pool, *sessionBytes, policy, relays, alicePub, bobPub, 15*time.Second)
	}()
	go func() {
		defer wg.Done()
		candidate, _, err := exchangeAnyMetered(relays, bobM, bobPub, alicePub, "", 15*time.Second)
		if err != nil {
			bobErr = err
			return
		}
		addr, err := peerEndpointFromCandidate(candidate)
		if err != nil {
			bobErr = fmt.Errorf("parsing alice's candidate %q: %w", candidate, err)
			return
		}
		if err := bob.SetPersistentKeepalive(alicePub, dataRelayKeepaliveSeconds); err != nil {
			bobErr = fmt.Errorf("arming bob's persistent keepalive: %w", err)
			return
		}
		bobEndpoint = &addr
	}()
	wg.Wait()
	if aliceErr != nil {
		return fmt.Errorf("alice's relayRoutedEndpoint: %w", aliceErr)
	}
	if bobErr != nil {
		return fmt.Errorf("bob's candidate exchange: %w", bobErr)
	}
	fmt.Fprintf(os.Stderr, "alice picked %s (the cheaper quote); bob adopted it\n", aliceLabel)

	if err := alice.AddPeer(bobPub, bobIP, aliceEndpoint); err != nil {
		return fmt.Errorf("alice.AddPeer: %w", err)
	}
	if err := bob.AddPeer(alicePub, aliceIP, bobEndpoint); err != nil {
		return fmt.Errorf("bob.AddPeer: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go func() {
		for {
			select {
			case <-pingCtx.Done():
				return
			default:
				_, _ = alice.Ping(bobAddr, 500*time.Millisecond)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	if !pingUntilConnectedForDemo(alice, bobAddr, 15*time.Second) {
		return fmt.Errorf("alice never reached bob through %s before the kill", aliceLabel)
	}
	fmt.Fprintln(os.Stderr, "real WireGuard handshake confirmed through the primary relay")

	aliceState := newRelayState()
	aliceState.set("bob", bobPub, aliceLabel)

	go watchDataRelayFailover(ctx, alice, aliceM, aliceState, "bob", bobIP, pool, *sessionBytes, policy, staticRendezvous(relays), alicePub, 15*time.Second)
	go watchDataRelayAdopt(ctx, bob, bobM, "alice", bobPub, alicePub, "", aliceIP, staticRendezvous(relays), nil)

	fmt.Fprintln(os.Stderr, ">>> kill the primary relay process now to trigger failover <<<")

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, aRelay, ok := aliceState.get("bob"); ok && aRelay == "backup" {
			fmt.Fprintln(os.Stderr, "alice failed over to the backup relay")
			if pingUntilConnectedForDemo(alice, bobAddr, 15*time.Second) {
				fmt.Fprintln(os.Stderr, "real WireGuard handshake confirmed again, through the backup relay")
				fmt.Fprintln(os.Stderr, "=== Gate 4.3: PASS ===")
				return nil
			}
			return fmt.Errorf("alice recorded the backup relay but the handshake never recovered")
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timed out waiting for alice to fail over to the backup relay")
}

func pingUntilConnectedForDemo(node *wgnode.Node, addr netip.Addr, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok, err := node.Ping(addr, time.Second); err == nil && ok {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
