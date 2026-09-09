package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
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
func startTestRendezvousRelay(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	type msg struct {
		Type      string `json:"type"`
		Pubkey    string `json:"pubkey,omitempty"`
		To        string `json:"to,omitempty"`
		From      string `json:"from,omitempty"`
		Candidate string `json:"candidate,omitempty"`
		Message   string `json:"message,omitempty"`
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

	return ln.Addr().String()
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
		_ = json.NewEncoder(w).Encode(map[string]string{"asset": "test", "amount": "1"})
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
	relayAddr := startTestRendezvousRelay(t)
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
		aliceEndpoint, aliceErr = relayRoutedEndpoint(alice, aliceM, pool, 1000, relayAddr, alicePub, bobPub, "", 5*time.Second)
	}()
	go func() {
		defer wg.Done()
		bobEndpoint, bobErr = relayRoutedEndpoint(bob, bobM, pool, 1000, relayAddr, bobPub, alicePub, "", 5*time.Second)
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
