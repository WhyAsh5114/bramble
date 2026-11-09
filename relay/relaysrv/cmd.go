package relaysrv

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func envInt(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

const gib = 1 << 30

// Main runs the relay with args (without the program name) and returns an exit code.
func Main(args []string, version string) int {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	if len(args) > 0 && args[0] == "pin" {
		dir := envStr("RELAY_STATE_DIR", "./state")
		key, err := LoadOrCreateKey(dir)
		if err != nil {
			fail(err)
		}
		pin, err := PinOf(&key.PublicKey)
		if err != nil {
			fail(err)
		}
		fmt.Println(pin)
		return 0
	}

	addr := fs.String("addr", envStr("RELAY_ADDR", ":9420"), "TCP address for rendezvous (TLS)")
	stunAddr := fs.String("stun", envStr("RELAY_STUN_ADDR", ":3478"), "UDP address for STUN; empty disables it")
	publicHost := fs.String("public-host", os.Getenv("RELAY_PUBLIC_HOST"), "public IP or DNS name peers reach data sessions at (required for the data relay)")
	dataHost := fs.String("data-host", envStr("RELAY_DATA_HOST", "0.0.0.0"), "interface data sessions bind to")
	portMin := fs.Int("port-min", int(envInt("RELAY_PORT_MIN", 40000)), "first UDP port for data sessions")
	portMax := fs.Int("port-max", int(envInt("RELAY_PORT_MAX", 40999)), "last UDP port for data sessions")
	stateDir := fs.String("state-dir", envStr("RELAY_STATE_DIR", "./state"), "directory for the TLS key and byte counters")
	perDay := fs.Int64("max-bytes-per-day", envInt("RELAY_MAX_BYTES_PER_DAY", 3*gib), "data relay byte budget per UTC day; 0 is unlimited")
	perMonth := fs.Int64("max-bytes-per-month", envInt("RELAY_MAX_BYTES_PER_MONTH", 80*gib), "data relay byte budget per UTC month; 0 is unlimited")
	sessionBytes := fs.Int64("session-bytes", envInt("RELAY_SESSION_BYTES", 512<<20), "cap of one data session")
	sessionTTL := fs.Duration("session-ttl", time.Duration(envInt("RELAY_SESSION_TTL_SECONDS", 1800))*time.Second, "lifetime of one data session")
	maxSessions := fs.Int("max-sessions", int(envInt("RELAY_MAX_SESSIONS", 256)), "concurrent data sessions")
	perHour := fs.Int("sessions-per-ip-per-hour", int(envInt("RELAY_SESSIONS_PER_IP_PER_HOUR", 60)), "new data sessions one source IP may open per hour")
	x402PayTo := fs.String("x402-pay-to", os.Getenv("RELAY_X402_PAY_TO"), "Hedera account that receives payment; setting it turns paid data sessions on (ADR 0021)")
	x402Price := fs.Int64("x402-price-per-mib", envInt("RELAY_X402_PRICE_PER_MIB", 0), "price of 1 MiB of data-relay traffic in the asset's smallest unit")
	x402Asset := fs.String("x402-asset", envStr("RELAY_X402_ASSET", "0.0.429274"), "Hedera token ID payment is made in (default: testnet USDC)")
	x402Network := fs.String("x402-network", envStr("RELAY_X402_NETWORK", "hedera:testnet"), "x402 network; only hedera:testnet is allowed")
	x402Facilitator := fs.String("x402-facilitator", envStr("RELAY_X402_FACILITATOR", "https://api.testnet.blocky402.com"), "x402 facilitator base URL")
	x402PublicURL := fs.String("x402-public-url", os.Getenv("RELAY_X402_PUBLIC_URL"), "https URL of this relay, named in 402 responses")
	hcsAccount := fs.String("hedera-account", os.Getenv("RELAY_HEDERA_ACCOUNT"), "Hedera testnet account that pays to write receipts; with -hedera-key and paid mode it turns on the HCS audit log (ADR 0021)")
	hcsKey := fs.String("hedera-key", os.Getenv("RELAY_HEDERA_KEY"), "hex ECDSA private key of -hedera-account; never printed or logged")
	hcsTopic := fs.String("hcs-topic", os.Getenv("RELAY_HCS_TOPIC"), "HCS topic for receipts; empty creates one that only this account can write to and remembers it in the state directory")
	noData := fs.Bool("rendezvous-only", os.Getenv("RELAY_RENDEZVOUS_ONLY") == "1", "offer rendezvous and STUN only, no data relay")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	key, err := LoadOrCreateKey(*stateDir)
	if err != nil {
		fail(fmt.Errorf("loading TLS key: %w", err))
	}
	pin, err := PinOf(&key.PublicKey)
	if err != nil {
		fail(err)
	}
	cfg, err := TLSConfig(key)
	if err != nil {
		fail(err)
	}

	var data *DataRelay
	b := NewBudget(filepath.Join(*stateDir, "budget.json"), *perDay, *perMonth)
	if !*noData {
		if *publicHost == "" {
			fail(errors.New("-public-host (RELAY_PUBLIC_HOST) is required for the data relay; use -rendezvous-only to run without it"))
		}
		if *portMin <= 0 || *portMax < *portMin || *portMax > 65535 {
			fail(errors.New("invalid data port range"))
		}
		data = NewDataRelay(*dataHost, Limits{
			SessionBytes: *sessionBytes, SessionTTL: *sessionTTL, MaxSessions: *maxSessions,
			SessionsPerHour: *perHour, PortMin: *portMin, PortMax: *portMax,
		}, b)
	}

	var paywall *Paywall
	var auditor *HCSAuditor
	if *x402PayTo != "" {
		if data == nil {
			fail(errors.New("paid data sessions need the data relay; drop -rendezvous-only"))
		}
		if *x402Network != "hedera:testnet" {
			fail(errors.New("-x402-network must be hedera:testnet; mainnet needs a new decision (ADR 0021)"))
		}
		paywall, err = NewPaywall(PaywallConfig{
			Facilitator: &Facilitator{BaseURL: *x402Facilitator}, Network: *x402Network, Asset: *x402Asset,
			PayTo: *x402PayTo, PricePerMiB: *x402Price, MaxSessionBytes: *sessionBytes, PublicURL: *x402PublicURL,
		}, *stateDir)
		if err != nil {
			fail(err)
		}
		if *hcsAccount != "" && *hcsKey != "" {
			auditor, err = NewHCSAuditor(*hcsAccount, *hcsKey, *hcsTopic, *stateDir)
			if err != nil {
				fail(err)
			}
			paywall.Audit, paywall.AuditTopic = auditor, auditor.Topic
			log.Printf("audit log: receipts go to HCS topic %s on hedera:testnet", auditor.Topic)
		}
	}

	ln, err := tls.Listen("tcp", *addr, cfg)
	if err != nil {
		fail(err)
	}
	log.Printf("relay %s: rendezvous on %s (TLS), pin %s", version, ln.Addr(), pin)
	if data != nil {
		log.Printf("data relay: sessions on %s:%d-%d, %d bytes each, budget %d/day %d/month", *publicHost, *portMin, *portMax, *sessionBytes, *perDay, *perMonth)
	}

	if *stunAddr != "" {
		ua, err := net.ResolveUDPAddr("udp", *stunAddr)
		if err != nil {
			fail(err)
		}
		uc, err := net.ListenUDP("udp", ua)
		if err != nil {
			fail(err)
		}
		log.Printf("stun on %s", uc.LocalAddr())
		go func() { _ = ServeSTUN(uc) }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := b.Save(); err != nil {
					log.Printf("saving budget: %v", err)
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		_ = b.Save()
		ln.Close()
	}()

	if auditor != nil {
		go auditor.Run(ctx)
	}
	srv := NewServer(data, *publicHost, version)
	if paywall != nil {
		log.Printf("paid data sessions: %d per MiB of %s on %s, paid to %s, facilitator %s", *x402Price, *x402Asset, *x402Network, *x402PayTo, *x402Facilitator)
		srv.SetPaywall(paywall)
	}
	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		fail(err)
	}
	return 0
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
