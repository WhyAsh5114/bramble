package main

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func dialTestServer(t *testing.T, srv *Server) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go srv.Serve(ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dialing relay: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func send(t *testing.T, conn net.Conn, m message) {
	t.Helper()
	if err := json.NewEncoder(conn).Encode(m); err != nil {
		t.Fatalf("sending %+v: %v", m, err)
	}
}

func recvWithTimeout(t *testing.T, conn net.Conn, timeout time.Duration) message {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("setting read deadline: %v", err)
	}
	var m message
	if err := json.NewDecoder(conn).Decode(&m); err != nil {
		t.Fatalf("receiving: %v", err)
	}
	return m
}

func TestOfferIsForwardedToRegisteredPeer(t *testing.T) {
	srv := NewServer()
	alice := dialTestServer(t, srv)
	bob := dialTestServer(t, srv)

	send(t, alice, message{Type: "hello", Pubkey: "alice-pubkey"})
	send(t, bob, message{Type: "hello", Pubkey: "bob-pubkey"})
	time.Sleep(50 * time.Millisecond) // let both registrations land

	send(t, alice, message{Type: "offer", To: "bob-pubkey", Candidate: "203.0.113.1:51820"})

	got := recvWithTimeout(t, bob, 2*time.Second)
	if got.Type != "offer" || got.From != "alice-pubkey" || got.Candidate != "203.0.113.1:51820" {
		t.Fatalf("unexpected message forwarded to bob: %+v", got)
	}
}

func TestSenderIsAckedOnceOfferIsForwarded(t *testing.T) {
	// The ack matters for correctness, not just convenience: without it, a
	// client that returns as soon as it receives the *other* side's offer
	// can close its connection before its own (possibly retried) offer ever
	// lands, stranding a peer waiting on it. See
	// brambled/rendezvous/client.go, which relies on this ack to know when
	// it's safe to stop retrying.
	srv := NewServer()
	alice := dialTestServer(t, srv)
	bob := dialTestServer(t, srv)

	send(t, alice, message{Type: "hello", Pubkey: "alice-pubkey"})
	send(t, bob, message{Type: "hello", Pubkey: "bob-pubkey"})
	time.Sleep(50 * time.Millisecond)

	send(t, alice, message{Type: "offer", To: "bob-pubkey", Candidate: "203.0.113.1:51820"})

	got := recvWithTimeout(t, alice, 2*time.Second)
	if got.Type != "ack" {
		t.Fatalf("expected an ack for a successfully forwarded offer, got: %+v", got)
	}
}

func TestOfferToUnregisteredPeerErrors(t *testing.T) {
	srv := NewServer()
	alice := dialTestServer(t, srv)

	send(t, alice, message{Type: "hello", Pubkey: "alice-pubkey"})
	send(t, alice, message{Type: "offer", To: "nobody-registered", Candidate: "203.0.113.1:51820"})

	got := recvWithTimeout(t, alice, 2*time.Second)
	if got.Type != "error" {
		t.Fatalf("expected an error reply for an unregistered target, got: %+v", got)
	}
}

func TestMeteredRelay_HelloWithoutTokenRejected(t *testing.T) {
	secret := mustSecret(t)
	srv := NewMeteredServer(secret)
	alice := dialTestServer(t, srv)

	send(t, alice, message{Type: "hello", Pubkey: "alice-pubkey"}) // no Token

	got := recvWithTimeout(t, alice, 2*time.Second)
	if got.Type != "error" {
		t.Fatalf("expected a metered relay to reject a tokenless hello, got: %+v", got)
	}
}

func TestMeteredRelay_HelloWithValidTokenAccepted(t *testing.T) {
	// Real relay-sidecar mints tokens over HTTP after a real payment settles
	// (docs/adr/0007); this test exercises the same wire format via
	// mintForTest (relay/token_test.go) so the protocol path is proven
	// without needing the TS sidecar or live chain — same "real component,
	// stubbed payment" shape as Gate 1.2/2.3's tests.
	secret := mustSecret(t)
	srv := NewMeteredServer(secret)
	alice := dialTestServer(t, srv)
	bob := dialTestServer(t, srv)

	token := mintForTest(secret, time.Now().Add(60*time.Second).Unix(), "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	send(t, alice, message{Type: "hello", Pubkey: "alice-pubkey", Token: token})
	send(t, bob, message{Type: "hello", Pubkey: "bob-pubkey", Token: token2(t, secret)})
	time.Sleep(50 * time.Millisecond)

	send(t, alice, message{Type: "offer", To: "bob-pubkey", Candidate: "203.0.113.1:51820"})

	got := recvWithTimeout(t, bob, 2*time.Second)
	if got.Type != "offer" || got.From != "alice-pubkey" {
		t.Fatalf("expected a paid connection attempt to work exactly like an unmetered one, got: %+v", got)
	}
}

// token2 mints a second, independently-nonced token for bob in
// TestMeteredRelay_HelloWithValidTokenAccepted — each side of an Exchange
// pays for and presents its own token (docs/adr/0007: both sides send their
// own hello/offer, so both sides pay).
func token2(t *testing.T, secret []byte) string {
	t.Helper()
	return mintForTest(secret, time.Now().Add(60*time.Second).Unix(), "ffffffffffffffffffffffffffffffff")
}

func TestMeteredRelay_ReplayedTokenRejectedOnSecondHello(t *testing.T) {
	secret := mustSecret(t)
	srv := NewMeteredServer(secret)
	token := mintForTest(secret, time.Now().Add(60*time.Second).Unix(), "11112222333344445555666677778888")

	// First use: hello succeeds, proven by getting the *admission-layer*
	// "not registered" error on a subsequent offer rather than a
	// payment-layer rejection (which closes the connection before any offer
	// would be processed at all).
	first := dialTestServer(t, srv)
	send(t, first, message{Type: "hello", Pubkey: "alice-pubkey", Token: token})
	send(t, first, message{Type: "offer", To: "nobody-registered", Candidate: "203.0.113.1:51820"})
	if got := recvWithTimeout(t, first, 2*time.Second); !strings.Contains(got.Message, "not registered") {
		t.Fatalf("expected the first hello to be accepted (peer-not-registered error, not a payment one): %+v", got)
	}

	// Second use of the same token, from a fresh connection, must be rejected.
	second := dialTestServer(t, srv)
	send(t, second, message{Type: "hello", Pubkey: "someone-else", Token: token})
	got := recvWithTimeout(t, second, 2*time.Second)
	if got.Type != "error" {
		t.Fatalf("expected a reused token on a second hello to be rejected, got: %+v", got)
	}
}

func TestRelayNeverInspectsOrAuthorizesCandidates(t *testing.T) {
	// The relay's only job is forwarding opaque strings. Assert it forwards a
	// value it can't possibly have validated (garbage, not a real ip:port)
	// unchanged — proving there is no authorization/validation logic to bypass.
	srv := NewServer()
	alice := dialTestServer(t, srv)
	bob := dialTestServer(t, srv)

	send(t, alice, message{Type: "hello", Pubkey: "alice-pubkey"})
	send(t, bob, message{Type: "hello", Pubkey: "bob-pubkey"})
	time.Sleep(50 * time.Millisecond)

	send(t, alice, message{Type: "offer", To: "bob-pubkey", Candidate: "not-a-real-address"})

	got := recvWithTimeout(t, bob, 2*time.Second)
	if got.Candidate != "not-a-real-address" {
		t.Fatalf("relay altered or rejected an opaque candidate blob: %+v", got)
	}
}
