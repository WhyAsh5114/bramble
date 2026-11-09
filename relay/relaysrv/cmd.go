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

	srv := NewServer(data, *publicHost, version)
	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		fail(err)
	}
	return 0
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
