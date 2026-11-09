// Command relay is a free, permissionless Bramble relay: rendezvous for sealed
// candidate exchange, a capped data relay for peers that cannot hole-punch,
// and a STUN responder. It sees only opaque blobs and WireGuard ciphertext;
// admission is always decided by the peers themselves (ADR 0003, ADR 0019).
package main

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

var version = "dev"

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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "pin" {
		dir := envStr("RELAY_STATE_DIR", "./state")
		key, err := loadOrCreateKey(dir)
		if err != nil {
			fail(err)
		}
		pin, err := pinOf(&key.PublicKey)
		if err != nil {
			fail(err)
		}
		fmt.Println(pin)
		return
	}

	addr := flag.String("addr", envStr("RELAY_ADDR", ":9420"), "TCP address for rendezvous (TLS)")
	stunAddr := flag.String("stun", envStr("RELAY_STUN_ADDR", ":3478"), "UDP address for STUN; empty disables it")
	publicHost := flag.String("public-host", os.Getenv("RELAY_PUBLIC_HOST"), "public IP or DNS name peers reach data sessions at (required for the data relay)")
	dataHost := flag.String("data-host", envStr("RELAY_DATA_HOST", "0.0.0.0"), "interface data sessions bind to")
	portMin := flag.Int("port-min", int(envInt("RELAY_PORT_MIN", 40000)), "first UDP port for data sessions")
	portMax := flag.Int("port-max", int(envInt("RELAY_PORT_MAX", 40999)), "last UDP port for data sessions")
	stateDir := flag.String("state-dir", envStr("RELAY_STATE_DIR", "./state"), "directory for the TLS key and byte counters")
	perDay := flag.Int64("max-bytes-per-day", envInt("RELAY_MAX_BYTES_PER_DAY", 3*gib), "data relay byte budget per UTC day; 0 is unlimited")
	perMonth := flag.Int64("max-bytes-per-month", envInt("RELAY_MAX_BYTES_PER_MONTH", 80*gib), "data relay byte budget per UTC month; 0 is unlimited")
	sessionBytes := flag.Int64("session-bytes", envInt("RELAY_SESSION_BYTES", 512<<20), "cap of one data session")
	sessionTTL := flag.Duration("session-ttl", time.Duration(envInt("RELAY_SESSION_TTL_SECONDS", 1800))*time.Second, "lifetime of one data session")
	maxSessions := flag.Int("max-sessions", int(envInt("RELAY_MAX_SESSIONS", 256)), "concurrent data sessions")
	perHour := flag.Int("sessions-per-ip-per-hour", int(envInt("RELAY_SESSIONS_PER_IP_PER_HOUR", 60)), "new data sessions one source IP may open per hour")
	noData := flag.Bool("rendezvous-only", os.Getenv("RELAY_RENDEZVOUS_ONLY") == "1", "offer rendezvous and STUN only, no data relay")
	flag.Parse()

	key, err := loadOrCreateKey(*stateDir)
	if err != nil {
		fail(fmt.Errorf("loading TLS key: %w", err))
	}
	pin, err := pinOf(&key.PublicKey)
	if err != nil {
		fail(err)
	}
	cfg, err := tlsConfig(key)
	if err != nil {
		fail(err)
	}

	var data *dataRelay
	b := newBudget(filepath.Join(*stateDir, "budget.json"), *perDay, *perMonth)
	if !*noData {
		if *publicHost == "" {
			fail(errors.New("-public-host (RELAY_PUBLIC_HOST) is required for the data relay; use -rendezvous-only to run without it"))
		}
		if *portMin <= 0 || *portMax < *portMin || *portMax > 65535 {
			fail(errors.New("invalid data port range"))
		}
		data = newDataRelay(*dataHost, dataLimits{
			SessionBytes: *sessionBytes, SessionTTL: *sessionTTL, MaxSessions: *maxSessions,
			SessionsPerHour: *perHour, PortMin: *portMin, PortMax: *portMax,
		}, b)
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
		go func() { _ = serveSTUN(uc) }()
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
				if err := b.save(); err != nil {
					log.Printf("saving budget: %v", err)
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		_ = b.save()
		ln.Close()
	}()

	srv := NewServer(data, *publicHost, version)
	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
