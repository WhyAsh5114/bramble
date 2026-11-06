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
	"github.com/WhyAsh5114/bramble/brambled/sponsor"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
	"golang.org/x/time/rate"
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
				// Deregister on disconnect, as relay/main.go's handle does.
				// Without this, an offer to a peer whose Exchange call has
				// already ended is "forwarded" to the dead connection and
				// acked, so the sender stops retrying and the peer never
				// sees it. The real relay replies "not registered" instead,
				// and the sender retries until the peer's next Exchange
				// registers (found 6 November 2026, ETH-86).
				defer func() {
					if pubkey == "" {
						return
					}
					mu.Lock()
					if peers[pubkey] == self {
						delete(peers, pubkey)
					}
					mu.Unlock()
				}()
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

	// ListenPort 0: the kernel picks a free port. Every endpoint here is a
	// relay address, so the test never needs its own port number, and a fixed
	// port collides when several `go test` processes share a host.
	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: 0, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting alice: %v", err)
	}
	defer alice.Close()
	bob, err := wgnode.New(wgnode.Config{PrivateKeyHex: bobPriv, ListenPort: 0, LocalAddress: bobAddr, Netstack: true})
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
		aliceEndpoint, _, aliceErr = relayRoutedEndpoint(alice, aliceM, pool, 1000, unrestrictedDataRelayPurchasePolicy, relays, alicePub, bobPub, 5*time.Second, nil)
	}()
	go func() {
		defer wg.Done()
		bobEndpoint, _, bobErr = relayRoutedEndpoint(bob, bobM, pool, 1000, unrestrictedDataRelayPurchasePolicy, relays, bobPub, alicePub, 5*time.Second, nil)
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
	testDataRelayFailover(t, staticRendezvous)
}

// The start-up relay discovery failed and a later query recovered the rendezvous set: both
// watchers must read the recovered set, so failover still works after the primary relay dies.
func TestDataRelayFailover_UsesRendezvousSetRecoveredAfterDiscoveryFailure(t *testing.T) {
	testDataRelayFailover(t, func(relays []sidecar.RendezvousRelay) rendezvousSource {
		var mu sync.Mutex
		calls := 0
		fetch := func() (*sidecar.RelaysResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if calls <= 1 {
				return nil, fmt.Errorf("sidecar returned 502: Invalid parameters")
			}
			return &sidecar.RelaysResponse{Rendezvous: relays}, nil
		}
		return newRendezvousPool(nil, fetch, 0).Get
	})
}

func testDataRelayFailover(t *testing.T, watcherSource func([]sidecar.RendezvousRelay) rendezvousSource) {
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

	// ListenPort 0: the kernel picks a free port. Every endpoint here is a
	// relay address, so the test never needs its own port number, and a fixed
	// port collides when several `go test` processes share a host.
	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: 0, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting alice: %v", err)
	}
	defer alice.Close()
	bob, err := wgnode.New(wgnode.Config{PrivateKeyHex: bobPriv, ListenPort: 0, LocalAddress: bobAddr, Netstack: true})
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
	var aliceSession *dataRelaySession
	var aliceErr, bobErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceEndpoint, aliceSession, aliceErr = relayRoutedEndpoint(alice, aliceM, pool, 1_000_000, unrestrictedDataRelayPurchasePolicy, relays, alicePub, bobPub, 15*time.Second, nil)
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
	if aliceSession.relayLabel != "primary" {
		t.Fatalf("expected alice to pick the cheaper relay first, got %q", aliceSession.relayLabel)
	}
	aliceState.set("bob", bobPub, aliceSession)

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
	go watchDataRelayFailover(ctx, alice, aliceM, aliceState, "bob", pool, 1_000_000, unrestrictedDataRelayPurchasePolicy, watcherSource(relays), alicePub, 15*time.Second)
	go watchDataRelayAdopt(ctx, bob, bobM, "alice", bobPub, alicePub, "", aliceIP, watcherSource(relays), nil)

	// Kill "the primary relay process": both its data relay and its
	// rendezvous listener together, matching a real relay/main.go process
	// running -meter -data-relay (docs/adr/0008).
	primaryRelay.stopAll()
	stopPrimaryRelay()

	deadline := time.Now().Add(50 * time.Second)
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

// An acknowledged candidate exchange is not a WireGuard handshake. A cloud
// adopter may still be waiting for admission or the shared sponsored poll
// budget. Keep its advertised session usable while it catches up.
func TestSponsoredRelay_BootstrapKeepsSessionUntilPeerHandshakes(t *testing.T) {
	t.Run("live allocation", func(t *testing.T) { testSponsoredRelayBootstrap(t, false, false) })
	t.Run("expired allocation", func(t *testing.T) { testSponsoredRelayBootstrap(t, true, false) })
	t.Run("depleted shared budget", func(t *testing.T) { testSponsoredRelayBootstrap(t, false, true) })
}

// Shorten only the first server-provided deadline, so expiry is tested without
// waiting five minutes. The relay and both WireGuard nodes remain real.
type expiringFirstSessionPayer struct {
	relayPayer
	shortened bool
}

func (p *expiringFirstSessionPayer) DataRelaySession(url string, bytes int64) (*sidecar.DataRelaySessionResponse, error) {
	response, err := p.relayPayer.DataRelaySession(url, bytes)
	if err == nil && !p.shortened {
		response.ExpiresAt = time.Now().Add(2 * time.Second).Unix()
		p.shortened = true
	}
	return response, err
}

func testSponsoredRelayBootstrap(t *testing.T, expireFirst, depletedBudget bool) {
	rdvAddr, sidecarURL := startRealSponsoringRelay(t, "device1,cloud1", 1<<20)
	relays := []sidecar.RendezvousRelay{{Label: "relay1", Address: rdvAddr, SidecarURL: sidecarURL}}
	pool := dataRelayFlag{"relay1": sidecarURL}
	devicePriv, devicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	cloudPriv, cloudPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	deviceIP := netip.MustParseAddr("10.100.0.1")
	cloudIP := netip.MustParseAddr("10.100.0.2")
	device, err := wgnode.New(wgnode.Config{PrivateKeyHex: devicePriv, LocalAddress: deviceIP, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	cloud, err := wgnode.New(wgnode.Config{PrivateKeyHex: cloudPriv, LocalAddress: cloudIP, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cloud.Close()
	payer := &countingPayer{relayPayer: &sponsor.Payer{Label: "device1"}}
	candidateCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		candidate, _, err := exchangeAnyMetered(relays, &sponsor.Payer{Label: "cloud1"}, cloudPub, devicePub, "", 15*time.Second)
		candidateCh <- candidate
		errCh <- err
	}()
	var bootstrapPayer relayPayer = payer
	if expireFirst {
		bootstrapPayer = &expiringFirstSessionPayer{relayPayer: payer}
	}
	endpoint, session, err := relayRoutedEndpoint(device, bootstrapPayer, pool, 10_000, unrestrictedDataRelayPurchasePolicy, relays, devicePub, cloudPub, 15*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate := <-candidateCh
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if candidate != endpoint.String() {
		t.Fatalf("initial endpoints differ: %s vs %s", endpoint, candidate)
	}
	if err := device.AddPeer(cloudPub, netip.PrefixFrom(cloudIP, 32), endpoint); err != nil {
		t.Fatal(err)
	}
	state := newRelayState()
	state.set("cloud1", cloudPub, session)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchDataRelayFailover(ctx, device, bootstrapPayer, state, "cloud1", pool, 10_000,
		unrestrictedDataRelayPurchasePolicy, staticRendezvous(relays), devicePub, 15*time.Second)
	// Hold cloud admission beyond the six-second stall timeout. No handshake
	// or received bytes exist yet, so that silence must not replace the session.
	time.Sleep(12 * time.Second)
	want := int32(1)
	if expireFirst {
		want = 2
	}
	if n := payer.sessions.Load(); n != want {
		t.Fatalf("bootstrap allocated %d sessions, want %d", n, want)
	}
	var pace *rate.Limiter
	if depletedBudget {
		pace, err = sponsor.PollLimiter(30)
		if err != nil {
			t.Fatal(err)
		}
		// Six inactive peers have consumed the shared startup burst.
		if !pace.AllowN(time.Now(), 6) {
			t.Fatal("missing initial six-poll burst")
		}
		reservation := pace.Reserve()
		delay := reservation.Delay()
		reservation.Cancel()
		if delay < 199*time.Second || delay > 201*time.Second {
			t.Fatalf("default depleted poll budget waits %s, want 200s", delay)
		}
		// Scale only test time. Production keeps the 200s interval; this test
		// needs one subsequent poll and never raises the relay's hourly cap.
		pace.SetLimit(rate.Every(15 * time.Second))
		stale := netip.MustParseAddrPort("127.0.0.1:9")
		if err := cloud.AddPeer(devicePub, netip.PrefixFrom(deviceIP, 32), &stale); err != nil {
			t.Fatal(err)
		}
	}
	go watchDataRelayAdopt(ctx, cloud, &sponsor.Payer{Label: "cloud1"}, "device1", cloudPub, devicePub, "",
		netip.PrefixFrom(deviceIP, 32), staticRendezvous(relays), pace)
	if !pingUntilConnected(t, device, cloudIP, 40*time.Second) {
		t.Fatal("delayed cloud adopter could not handshake through the retained session")
	}
}

// A renewal starts with the previous session's received-byte counter still
// positive. That counter must not confirm the new allocation before adoption.
func TestSponsoredRelay_RenewalKeepsSessionWhileAdopterWaits(t *testing.T) {
	p := connectSponsoredPair(t, 64<<10, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "after delayed adoption")
	}))
	if status, _, err := getUntil(p.httpClient(time.Second), "http://cloud1.mesh/", 10*time.Second); err != nil || status != http.StatusOK {
		t.Fatalf("initial GET: %d %v", status, err)
	}
	// Take the admitted cloud peer offline after receiving real traffic. This
	// stops keepalives deterministically, without depending on a download's
	// packet sizes leaving room for a few keepalives below the byte cap.
	if err := p.cloud.RemovePeer(p.devicePub); err != nil {
		t.Fatal(err)
	}
	payer := &countingPayer{relayPayer: &sponsor.Payer{Label: "device1"}}
	payer.sessions.Store(1)
	state := newRelayState()
	state.set("cloud1", p.cloudPub, p.session)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchDataRelayFailover(ctx, p.device, payer, state, "cloud1", p.pool, 10_000,
		unrestrictedDataRelayPurchasePolicy, staticRendezvous(p.relays), p.devicePub, 3*time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for payer.sessions.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := payer.sessions.Load(); n != 2 {
		t.Fatalf("expected first renewal, got %d sessions", n)
	}
	// Cross another complete stall plus failed-publication cycle.
	time.Sleep(14 * time.Second)
	if n := payer.sessions.Load(); n != 2 {
		t.Fatalf("unadopted renewal churned: allocated %d sessions, want 2", n)
	}
	go watchDataRelayAdopt(ctx, p.cloud, &sponsor.Payer{Label: "cloud1"}, "device1", p.cloudPub, p.devicePub, "",
		netip.PrefixFrom(p.deviceIP, 32), staticRendezvous(p.relays), nil)
	if status, body, err := getUntil(p.httpClient(time.Second), "http://cloud1.mesh/", 25*time.Second); err != nil || status != http.StatusOK || body != "after delayed adoption" {
		t.Fatalf("GET after adoption: %d %q %v", status, body, err)
	}
	if n := payer.sessions.Load(); n != 2 {
		t.Fatalf("recovery needed %d sessions, want 2", n)
	}
}

// Pause the external allocation response to model admission or traffic changing
// while a relay request is in flight.
type pausedSessionPayer struct {
	relayPayer
	allocated chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (p *pausedSessionPayer) DataRelaySession(url string, bytes int64) (*sidecar.DataRelaySessionResponse, error) {
	response, err := p.relayPayer.DataRelaySession(url, bytes)
	p.once.Do(func() { close(p.allocated); <-p.release })
	return response, err
}

func TestSponsoredRelay_FailoverRechecksAdmissionAndTrafficAfterAllocation(t *testing.T) {
	for _, revoked := range []bool{true, false} {
		name := "recovered traffic"
		if revoked {
			name = "revoked peer"
		}
		t.Run(name, func(t *testing.T) {
			p := connectSponsoredPair(t, 1<<20, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "connected") }))
			if status, _, err := getUntil(p.httpClient(time.Second), "http://cloud1.mesh/", 10*time.Second); err != nil || status != http.StatusOK {
				t.Fatalf("initial GET: %d %v", status, err)
			}
			if err := p.cloud.RemovePeer(p.devicePub); err != nil {
				t.Fatal(err)
			}
			payer := &pausedSessionPayer{relayPayer: &sponsor.Payer{Label: "device1"}, allocated: make(chan struct{}), release: make(chan struct{})}
			state := newRelayState()
			state.set("cloud1", p.cloudPub, p.session)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go watchDataRelayFailover(ctx, p.device, payer, state, "cloud1", p.pool, 10_000,
				unrestrictedDataRelayPurchasePolicy, staticRendezvous(p.relays), p.devicePub, 3*time.Second)
			select {
			case <-payer.allocated:
			case <-time.After(20 * time.Second):
				close(payer.release)
				t.Fatal("no replacement allocation after the stall")
			}
			// Release even when an assertion below fails.
			var release sync.Once
			resume := func() { release.Do(func() { close(payer.release) }) }
			defer resume()
			if revoked {
				if err := p.device.RemovePeer(p.cloudPub); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := p.cloud.SetPersistentKeepalive(p.devicePub, dataRelayKeepaliveSeconds); err != nil {
					t.Fatal(err)
				}
				if err := p.cloud.AddPeer(p.devicePub, netip.PrefixFrom(p.deviceIP, 32), p.deviceEndpoint); err != nil {
					t.Fatal(err)
				}
				if status, _, err := getUntil(p.httpClient(time.Second), "http://cloud1.mesh/", 10*time.Second); err != nil || status != http.StatusOK {
					t.Fatalf("GET while allocation waits: %d %v", status, err)
				}
			}
			resume()
			// Observe the peer table through a full watcher turn after the request.
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				peers, err := p.device.Peers()
				if err != nil {
					t.Fatal(err)
				}
				for _, peer := range peers {
					if revoked && peer.PublicKeyHex == p.cloudPub {
						t.Fatal("relay completion restored a revoked peer")
					}
				}
				if !revoked && state.session("cloud1", p.cloudPub) != p.session {
					t.Fatal("old-session recovery was credited to a replacement allocation")
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func TestDataRelayEndpointUpdatePreservesAdmission(t *testing.T) {
	private, _, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_, peerKey, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	node, err := wgnode.New(wgnode.Config{PrivateKeyHex: private, LocalAddress: netip.MustParseAddr("10.100.0.1"), Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	endpoint := netip.MustParseAddrPort("127.0.0.1:9")
	route := netip.MustParsePrefix("10.100.0.2/32")
	if err := node.AddPeer(peerKey, route, nil); err != nil {
		t.Fatal(err)
	}
	if err := node.UpdatePeerEndpoint(peerKey, endpoint, dataRelayKeepaliveSeconds); err != nil {
		t.Fatal(err)
	}
	peers, err := node.Peers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].Endpoint != endpoint.String() || len(peers[0].AllowedIPs) != 1 || peers[0].AllowedIPs[0] != route.String() {
		t.Fatalf("transport update changed admission: %+v", peers)
	}
	if err := node.RemovePeer(peerKey); err != nil {
		t.Fatal(err)
	}
	if err := node.UpdatePeerEndpoint(peerKey, endpoint, dataRelayKeepaliveSeconds); err != nil {
		t.Fatal(err)
	}
	peers, err = node.Peers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Fatalf("transport update restored a revoked peer: %+v", peers)
	}
}
