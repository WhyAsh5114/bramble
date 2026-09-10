package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// startTestRendezvousRelay is a minimal honest rendezvous relay, hand-rolled
// for this test rather than imported — relay/ is a separate Go module
// (package main there too), so it can't be imported here, matching the
// existing precedent in rendezvous/gate1_2_test.go's startHostileRelay.
// Unlike that one, this always forwards truthfully.
//
// requiredTokenPrefix, if non-empty, gates every hello exactly like a real
// metered relay does (relay/main.go's tokenVerifier): a hello whose Token
// doesn't start with this relay's own prefix, OR whose exact Token value
// has already been presented once before (single-use, matching a real
// token's nonce-consumption check), gets an error reply and the connection
// is closed, never registered. Both checks matter for
// TestDataRelayFailover_SwitchesToBackupRelayWhenPrimaryStalls below to be
// a real regression test for docs/adr/0008's fix: the prefix check catches
// a token minted for the wrong relay being presented here, and the
// single-use check catches a token being reused across Exchange calls
// (rendezvous.Exchange's own documented "one token per call" contract) —
// an always-accept, or reuse-tolerant, fake would let either bug slide.
// The returned stop func closes the listener early — a real relay's
// rendezvous TCP listener dies the instant the relay process is killed
// (relay/main.go has no SIGTERM handler, but the OS reclaims the socket
// regardless), so a test simulating "kill the primary relay" needs to be
// able to take this down at the same moment it stops the paired data relay,
// not just at t.Cleanup.
func startTestRendezvousRelay(t *testing.T, requiredTokenPrefix string) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	var seenMu sync.Mutex
	seenTokens := make(map[string]bool)

	type msg struct {
		Type      string `json:"type"`
		Pubkey    string `json:"pubkey,omitempty"`
		To        string `json:"to,omitempty"`
		From      string `json:"from,omitempty"`
		Candidate string `json:"candidate,omitempty"`
		Message   string `json:"message,omitempty"`
		Token     string `json:"token,omitempty"`
	}
	type fakePeer struct {
		mu  sync.Mutex
		enc *json.Encoder
	}
	send := func(p *fakePeer, m msg) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.enc.Encode(m)
	}

	var mu sync.Mutex
	peers := make(map[string]*fakePeer)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				self := &fakePeer{enc: json.NewEncoder(conn)}
				dec := json.NewDecoder(conn)
				var pubkey string
				for {
					var m msg
					if err := dec.Decode(&m); err != nil {
						return
					}
					switch m.Type {
					case "hello":
						if requiredTokenPrefix != "" {
							seenMu.Lock()
							valid := strings.HasPrefix(m.Token, requiredTokenPrefix+"-") && !seenTokens[m.Token]
							if valid {
								seenTokens[m.Token] = true
							}
							seenMu.Unlock()
							if !valid {
								_ = send(self, msg{Type: "error", Message: "invalid or reused token"})
								return
							}
						}
						pubkey = m.Pubkey
						mu.Lock()
						peers[pubkey] = self
						mu.Unlock()
					case "offer":
						mu.Lock()
						target, ok := peers[m.To]
						mu.Unlock()
						if !ok {
							_ = send(self, msg{Type: "error", Message: "peer not registered"})
							continue
						}
						_ = send(target, msg{Type: "offer", From: pubkey, Candidate: m.Candidate})
						_ = send(self, msg{Type: "ack"})
					}
				}
			}()
		}
	}()

	return ln.Addr().String(), func() { ln.Close() }
}

// startTestDataRelay is a minimal address-learning UDP forwarder — the same
// design as relay/datarelay.go's forward() (first two distinct sources
// become the two slots, forwarded between unconditionally), reimplemented
// here since relay/ is a separate module. No byte accounting: this test
// only cares about whether both sides' endpoints end up pointed at the same
// socket, which is exactly the wiring datarelay_test.go's own module can't
// check (it never touches brambled/main.go's EndpointResolver logic).
func startTestDataRelay(t *testing.T) *netip.AddrPort {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		var slots [2]*net.UDPAddr
		buf := make([]byte, 2048)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var other *net.UDPAddr
			matched := false
			for i, s := range slots {
				if s != nil && s.IP.Equal(src.IP) && s.Port == src.Port {
					other = slots[1-i]
					slots[i] = src
					matched = true
					break
				}
			}
			if !matched {
				for i, s := range slots {
					if s == nil {
						slots[i] = src
						other = slots[1-i]
						break
					}
				}
			}
			if other != nil {
				_, _ = conn.WriteToUDP(buf[:n], other)
			}
		}
	}()

	addr := netip.MustParseAddrPort(conn.LocalAddr().String())
	return &addr
}

// startFakeDataRelaySidecar stands in for relay-sidecar's public
// GET /price and for what a local ENS sidecar's POST /data-relay-session
// would return, skipping real x402/Hedera payment entirely — this test is
// about whether relayRoutedEndpoint wires both sides to the same socket,
// not about payment, which datarelay_test.go and pricing.test.ts already
// cover on their own.
func startFakeDataRelaySidecar(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/price", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"asset": "0.0.429274", "amount": "1"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// startFakeENSSidecar stands in for a node's local ENS sidecar's
// POST /data-relay-session — always hands back a session pointing at
// relayPort, regardless of request body (real payment is out of scope
// here; sidecar/src/payments/datarelay.ts already covers that call).
func startFakeENSSidecar(t *testing.T, relayPort int) *sidecar.Manager {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/data-relay-session", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sessionId": "test-session",
			"port":      relayPort,
			"expiresAt": time.Now().Add(time.Minute).Unix(),
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var port int
	if _, err := fmt.Sscanf(srv.URL, "http://127.0.0.1:%d", &port); err != nil {
		t.Fatalf("parsing fake sidecar port from %q: %v", srv.URL, err)
	}
	return &sidecar.Manager{Port: port}
}

// TestRelayRoutedEndpoint_BothSidesReachEachOtherThroughDataRelay is a
// regression test for exactly the bug a review caught before any live run:
// relayRoutedEndpoint must return the relay's own address as this node's
// endpoint, never the peer's candidate from Exchange's return value — a
// peer with no direct address (candidate "") would otherwise leave this
// node's own endpoint unset, and nothing would ever reach the relay at all
// (docs/adr/0008). Both Alice and Bob here are configured relay-routed,
// mirroring the two-NAT demo topology Gate 4.3 requires.
func TestRelayRoutedEndpoint_BothSidesReachEachOtherThroughDataRelay(t *testing.T) {
	relayAddr, _ := startTestRendezvousRelay(t, "") // unmetered
	relays := []sidecar.RendezvousRelay{{Label: "test-relay", Address: relayAddr}}
	dataRelay := startTestDataRelay(t)
	sidecarURL := startFakeDataRelaySidecar(t)
	pool := dataRelayFlag{"test-relay": sidecarURL}

	aliceM := startFakeENSSidecar(t, int(dataRelay.Port()))
	bobM := startFakeENSSidecar(t, int(dataRelay.Port()))

	alicePriv, alicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating alice's key pair: %v", err)
	}
	bobPriv, bobPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating bob's key pair: %v", err)
	}

	aliceAddr := netip.MustParseAddr("10.99.0.1")
	bobAddr := netip.MustParseAddr("10.99.0.2")

	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: 61950, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting alice: %v", err)
	}
	defer alice.Close()
	bob, err := wgnode.New(wgnode.Config{PrivateKeyHex: bobPriv, ListenPort: 61951, LocalAddress: bobAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting bob: %v", err)
	}
	defer bob.Close()

	// Exchange (inside relayRoutedEndpoint) has each side wait for the
	// other's offer, so both calls must run concurrently — exactly how two
	// real nodes would call this at connection-setup time, and how
	// rendezvous/gate1_2_test.go's own Exchange usage is documented to
	// behave ("won't generally call this at the exact same instant", hence
	// the relay's retry/ack protocol).
	var aliceEndpoint, bobEndpoint *netip.AddrPort
	var aliceErr, bobErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceEndpoint, _, aliceErr = relayRoutedEndpoint(alice, aliceM, pool, 1000, unrestrictedDataRelayPurchasePolicy, relays, alicePub, bobPub, 5*time.Second)
	}()
	go func() {
		defer wg.Done()
		bobEndpoint, _, bobErr = relayRoutedEndpoint(bob, bobM, pool, 1000, unrestrictedDataRelayPurchasePolicy, relays, bobPub, alicePub, 5*time.Second)
	}()
	wg.Wait()
	if aliceErr != nil {
		t.Fatalf("alice's relayRoutedEndpoint: %v", aliceErr)
	}
	if bobErr != nil {
		t.Fatalf("bob's relayRoutedEndpoint: %v", bobErr)
	}

	// The bug this test guards against: if either endpoint came from the
	// peer's own published candidate instead of the relay's address, the
	// two would diverge (or be nil), and no forwarding would ever happen.
	if aliceEndpoint.String() != bobEndpoint.String() {
		t.Fatalf("alice and bob resolved different relay endpoints: %s vs %s", aliceEndpoint, bobEndpoint)
	}

	if err := alice.AddPeer(bobPub, netip.PrefixFrom(bobAddr, 32), aliceEndpoint); err != nil {
		t.Fatalf("alice adding bob: %v", err)
	}
	if err := bob.AddPeer(alicePub, netip.PrefixFrom(aliceAddr, 32), bobEndpoint); err != nil {
		t.Fatalf("bob adding alice: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		ok, err = alice.Ping(bobAddr, time.Second)
		if err == nil && ok {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ok {
		t.Fatal("alice and bob failed to complete a handshake through the data relay")
	}
}

// startTestDataRelayStoppable is startTestDataRelay, but returns a stop
// function so a test can kill it mid-run (docs/05_BUILD_PLAN.md Gate 4.3's
// "kill the selected relay mid-transfer") instead of only at test cleanup.
// fakeDataRelay simulates relay/datarelay.go's real behavior closely
// enough to catch the bug a fixed-port stub hides: every allocate() call
// opens a genuinely fresh UDP forwarder, exactly like the real Go relay
// allocates a fresh port per session rather than reusing one across calls.
// A first draft of this test's fake sidecar returned the same fixed port
// for every call to a given sidecar URL, which let both sides of a pair
// independently "buy" and still land on the same socket by coincidence —
// a real relay never does that, and a live run against the real relay
// binary is what caught it (docs/adr/0008, "Kill-mid-transfer failover").
type fakeDataRelay struct {
	mu      sync.Mutex
	conns   []*net.UDPConn
	stopped bool
}

func newFakeDataRelay() *fakeDataRelay { return &fakeDataRelay{} }

// allocate opens a fresh UDP forwarder and returns its port, or ok=false if
// this relay has been stopped — a real relay process's HTTP sidecar dies
// along with everything else when killed, so a killed relay can't hand out
// new sessions either; a stub that only stopped forwarding UDP but kept
// granting new sessions let this test bounce back to a "dead" relay it
// should have excluded for good, which real process death never allows.
func (f *fakeDataRelay) allocate(t *testing.T) (int, bool) {
	t.Helper()
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return 0, false
	}
	f.mu.Unlock()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("fakeDataRelay: listening: %v", err)
	}
	f.mu.Lock()
	f.conns = append(f.conns, conn)
	f.mu.Unlock()

	go func() {
		var slots [2]*net.UDPAddr
		buf := make([]byte, 2048)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var other *net.UDPAddr
			matched := false
			for i, s := range slots {
				if s != nil && s.IP.Equal(src.IP) && s.Port == src.Port {
					other = slots[1-i]
					slots[i] = src
					matched = true
					break
				}
			}
			if !matched {
				for i, s := range slots {
					if s == nil {
						slots[i] = src
						other = slots[1-i]
						break
					}
				}
			}
			if other != nil {
				_, _ = conn.WriteToUDP(buf[:n], other)
			}
		}
	}()

	return conn.LocalAddr().(*net.UDPAddr).Port, true
}

// stopAll closes every session this relay ever allocated and marks it
// stopped so it can never allocate another — simulates killing the whole
// relay process mid-transfer, not just one session's socket.
func (f *fakeDataRelay) stopAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	for _, c := range f.conns {
		c.Close()
	}
}

// startFakeDataRelaySidecarPriced is startFakeDataRelaySidecar with a
// caller-chosen price, so a test can make datarelay.Pick's choice
// deterministic across multiple fake relays instead of racing on identical
// prices (whose tie would be broken by Go's unspecified map iteration
// order, not the score itself).
func startFakeDataRelaySidecarPriced(t *testing.T, priceAmount string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/price", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"asset": "0.0.429274", "amount": priceAmount})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// startFakeENSSidecarMulti is startFakeENSSidecar for a pool of more than
// one candidate relay: it reads the request's sidecarUrl and allocates a
// fresh session from that specific fakeDataRelay, instead of always
// answering with one fixed port regardless of how many times it's called.
// It also serves /rendezvous-token, minting a fresh, unique token per call
// — prefixed with whichever opaque secret tokenByURL says belongs to the
// requested sidecarUrl, suffixed with a nanosecond timestamp (unique enough
// across the two independent counters startFakeENSSidecarMulti's alice and
// bob instances each keep — a real token's nonce is random for the same
// reason, just cryptographically so rather than merely improbable) —
// mirroring a real relay-sidecar's token in two ways at once
// (docs/adr/0008): it's only ever meaningful to the one rendezvous relay
// that shares its secret (the prefix, checked by
// startTestRendezvousRelay's requiredTokenPrefix), and it's good for
// exactly one Exchange call (the suffix, checked there via its seenTokens
// set) — a real relay's tokenVerifier rejects a reused nonce the same way.
func startFakeENSSidecarMulti(t *testing.T, relayByURL map[string]*fakeDataRelay, tokenByURL map[string]string) *sidecar.Manager {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/data-relay-session", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SidecarURL string `json:"sidecarUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		relay, ok := relayByURL[body.SidecarURL]
		if !ok {
			http.Error(w, fmt.Sprintf("no fake relay configured for sidecarUrl %q", body.SidecarURL), http.StatusBadRequest)
			return
		}
		port, ok := relay.allocate(t)
		if !ok {
			http.Error(w, "relay is stopped", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sessionId": fmt.Sprintf("test-session-%d-%d", port, time.Now().UnixNano()),
			"port":      port,
			"expiresAt": time.Now().Add(time.Minute).Unix(),
		})
	})
	mux.HandleFunc("/rendezvous-token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SidecarURL string `json:"sidecarUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		prefix, ok := tokenByURL[body.SidecarURL]
		if !ok {
			http.Error(w, fmt.Sprintf("no fake token configured for sidecarUrl %q", body.SidecarURL), http.StatusBadRequest)
			return
		}
		token := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":     token,
			"expiresAt": time.Now().Add(time.Minute).Unix(),
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var port int
	if _, err := fmt.Sscanf(srv.URL, "http://127.0.0.1:%d", &port); err != nil {
		t.Fatalf("parsing fake sidecar port from %q: %v", srv.URL, err)
	}
	return &sidecar.Manager{Port: port}
}

// TestDataRelayFailover_SwitchesToBackupRelayWhenPrimaryStalls is Gate
// 4.3's kill-mid-transfer failover: two real data relays, alice picked the
// cheaper one (deterministic via priced quotes), a real handshake and
// traffic flowing through it, then the primary relay is killed mid-flight
// — its rendezvous listener along with its data relay, since a real relay
// process serves both (relay/main.go's -meter -data-relay). Each of the two
// rendezvous relays here requires its own distinct token, exactly like two
// real relay processes each generating their own random secret
// (relay/sidecar.go) — this is what makes this test a real regression test
// for docs/adr/0008's fix: before it, a token bought from the primary's
// sidecar was presented to the backup's rendezvous listener too and
// rejected, since ExchangeAny reused one token across every relay address.
// watchDataRelayFailover must detect the stall via real RxBytes, buy a
// *fresh* token from the backup's own sidecar, and re-point alice's own
// peer-endpoint at the backup relay — proven by a real WireGuard handshake
// succeeding again afterward, not just a state flag flipping.
func TestDataRelayFailover_SwitchesToBackupRelayWhenPrimaryStalls(t *testing.T) {
	primaryRelayAddr, stopPrimaryRelay := startTestRendezvousRelay(t, "primary-secret")
	backupRelayAddr, _ := startTestRendezvousRelay(t, "backup-secret")

	primaryRelay := newFakeDataRelay()
	backupRelay := newFakeDataRelay()

	sidecarURLPrimary := startFakeDataRelaySidecarPriced(t, "1")   // cheapest — Pick chooses this first
	sidecarURLBackup := startFakeDataRelaySidecarPriced(t, "1000") // pricier — only used on failover

	pool := dataRelayFlag{"primary": sidecarURLPrimary, "backup": sidecarURLBackup}
	relayByURL := map[string]*fakeDataRelay{
		sidecarURLPrimary: primaryRelay,
		sidecarURLBackup:  backupRelay,
	}
	// The same sidecarUrl each relay already uses for /data-relay-session
	// also serves /rendezvous-token in the real system — one relay-sidecar
	// process per relay, offering both roles (relay/main.go's -meter
	// -data-relay).
	tokenByURL := map[string]string{
		sidecarURLPrimary: "primary-secret",
		sidecarURLBackup:  "backup-secret",
	}
	relays := []sidecar.RendezvousRelay{
		{Label: "primary", Address: primaryRelayAddr, SidecarURL: sidecarURLPrimary},
		{Label: "backup", Address: backupRelayAddr, SidecarURL: sidecarURLBackup},
	}

	aliceM := startFakeENSSidecarMulti(t, relayByURL, tokenByURL)
	bobM := startFakeENSSidecarMulti(t, relayByURL, tokenByURL)

	alicePriv, alicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating alice's key pair: %v", err)
	}
	bobPriv, bobPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating bob's key pair: %v", err)
	}

	aliceAddr := netip.MustParseAddr("10.100.0.1")
	bobAddr := netip.MustParseAddr("10.100.0.2")
	aliceIP := netip.PrefixFrom(aliceAddr, 32)
	bobIP := netip.PrefixFrom(bobAddr, 32)

	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: 61960, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting alice: %v", err)
	}
	defer alice.Close()
	bob, err := wgnode.New(wgnode.Config{PrivateKeyHex: bobPriv, ListenPort: 61961, LocalAddress: bobAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting bob: %v", err)
	}
	defer bob.Close()

	aliceState := newRelayState()

	// Only alice is -relay-peer for bob (docs/adr/0008: "only the flagged
	// side buys a session; the other side needs no config at all"). A
	// real relay hands out a fresh port per session bought, so both sides
	// independently buying — this test's old shape — only ever worked
	// because the old fake stub returned the same fixed port for every
	// call to a given sidecar URL, which the real relay binary never
	// does (caught by a live run against it, not by this suite). Bob just
	// adopts whatever alice publishes, the same as any ordinary
	// non-relay-peer.
	var aliceEndpoint, bobEndpoint *netip.AddrPort
	var aliceLabel string
	var aliceErr, bobErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceEndpoint, aliceLabel, aliceErr = relayRoutedEndpoint(alice, aliceM, pool, 1_000_000, unrestrictedDataRelayPurchasePolicy, relays, alicePub, bobPub, 15*time.Second)
	}()
	go func() {
		defer wg.Done()
		candidate, _, err := exchangeAnyMetered(relays, bobM, bobPub, alicePub, "", 15*time.Second)
		if err != nil {
			bobErr = err
			return
		}
		addr, err := netip.ParseAddrPort(candidate)
		if err != nil {
			bobErr = fmt.Errorf("parsing alice's candidate %q: %w", candidate, err)
			return
		}
		// The relay only learns an address from outbound traffic it
		// observes (docs/adr/0008) — bob has no way to know alice's
		// candidate is relay-routed rather than a real direct address, so
		// this has to be unconditional, same as relayRoutedEndpoint's own
		// side of the pair.
		if err := bob.SetPersistentKeepalive(alicePub, dataRelayKeepaliveSeconds); err != nil {
			bobErr = fmt.Errorf("arming bob's persistent keepalive: %w", err)
			return
		}
		bobEndpoint = &addr
	}()
	wg.Wait()
	if aliceErr != nil {
		t.Fatalf("alice's relayRoutedEndpoint: %v", aliceErr)
	}
	if bobErr != nil {
		t.Fatalf("bob's candidate exchange: %v", bobErr)
	}
	if aliceLabel != "primary" {
		t.Fatalf("expected alice to pick the cheaper relay first, got %q", aliceLabel)
	}
	aliceState.set("bob", bobPub, aliceLabel)

	if err := alice.AddPeer(bobPub, bobIP, aliceEndpoint); err != nil {
		t.Fatalf("alice adding bob: %v", err)
	}
	if err := bob.AddPeer(alicePub, aliceIP, bobEndpoint); err != nil {
		t.Fatalf("bob adding alice: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Continuous traffic — this is what makes a stall meaningful (per
	// WaitForStall's own doc comment) and what proves the connection
	// actually recovers, not just that state flipped.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, _ = alice.Ping(bobAddr, 500*time.Millisecond)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	if !pingUntilConnected(t, alice, bobAddr, 8*time.Second) {
		t.Fatal("alice and bob failed to complete an initial handshake through the primary relay")
	}

	// Alice (the buying side) gets watchDataRelayFailover; bob (the
	// adopting side) gets watchDataRelayAdopt — the asymmetric pairing
	// runServe now wires for real (main.go's non-relay-peer watcher loop),
	// not two independent buyers racing for the same lucky port.
	go watchDataRelayFailover(ctx, alice, aliceM, aliceState, "bob", bobIP, pool, 1_000_000, unrestrictedDataRelayPurchasePolicy, relays, alicePub, 15*time.Second)
	go watchDataRelayAdopt(ctx, bob, bobM, "alice", bobPub, alicePub, "", aliceIP, relays)

	// Kill "the primary relay process": both its data relay and its
	// rendezvous listener together, matching a real relay/main.go process
	// running -meter -data-relay (docs/adr/0008).
	primaryRelay.stopAll()
	stopPrimaryRelay()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, aliceSide, ok := aliceState.get("bob"); ok && aliceSide == "backup" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, label, _ := aliceState.get("bob"); label != "backup" {
		t.Fatalf("alice never failed over to the backup relay (still recorded as %q)", label)
	}

	if !pingUntilConnected(t, alice, bobAddr, 10*time.Second) {
		t.Fatal("alice and bob failed to reconnect through the backup relay after failover")
	}
}

// pingUntilConnected polls Ping until it succeeds or the deadline passes.
func pingUntilConnected(t *testing.T, node *wgnode.Node, addr netip.Addr, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok, err := node.Ping(addr, time.Second); err == nil && ok {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
