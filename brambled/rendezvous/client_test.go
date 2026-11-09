package rendezvous

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

// startFakeRelay starts the real relay on loopback and returns it pinned.
func startFakeRelay(t *testing.T) Relay {
	t.Helper()
	l, err := relaysrv.StartLocal(relaysrv.Limits{SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 8, SessionsPerHour: 100, PortMin: 47000, PortMax: 47099}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return Relay{Label: "test", Address: l.Addr, Pin: l.Pin}
}

// identity registers a fresh node key and returns its public key.
func identity(t *testing.T) string {
	t.Helper()
	priv, _, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := RegisterIdentity(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestExchangeBothSidesCallSimultaneously(t *testing.T) {
	relay := startFakeRelay(t)
	alice, bob := identity(t), identity(t)
	type res struct {
		c   string
		err error
	}
	a, b := make(chan res, 1), make(chan res, 1)
	go func() {
		c, err := Exchange(relay, alice, bob, "ep1:srflx=203.0.113.1:51820", 5*time.Second)
		a <- res{c, err}
	}()
	go func() {
		c, err := Exchange(relay, bob, alice, "ep1:srflx=203.0.113.2:51820", 5*time.Second)
		b <- res{c, err}
	}()
	ra, rb := <-a, <-b
	if ra.err != nil || rb.err != nil {
		t.Fatalf("alice %v, bob %v", ra.err, rb.err)
	}
	if ra.c != "ep1:srflx=203.0.113.2:51820" || rb.c != "ep1:srflx=203.0.113.1:51820" {
		t.Fatalf("alice got %q, bob got %q", ra.c, rb.c)
	}
}

func TestExchangeRetriesUntilPeerArrives(t *testing.T) {
	relay := startFakeRelay(t)
	alice, bob := identity(t), identity(t)
	done := make(chan string, 1)
	go func() {
		c, err := Exchange(relay, alice, bob, "a", 6*time.Second)
		if err != nil {
			t.Error(err)
		}
		done <- c
	}()
	time.Sleep(1500 * time.Millisecond)
	go func() { _, _ = Exchange(relay, bob, alice, "b", 6*time.Second) }()
	if got := <-done; got != "b" {
		t.Fatalf("alice got %q", got)
	}
}

func TestExchangeTimesOutIfPeerNeverArrives(t *testing.T) {
	relay := startFakeRelay(t)
	alice, nobody := identity(t), identity(t)
	if _, err := Exchange(relay, alice, nobody, "a", 2*time.Second); err == nil {
		t.Fatal("expected a timeout")
	}
}

func TestExchangeRefusesAWrongPin(t *testing.T) {
	relay := startFakeRelay(t)
	relay.Pin = "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	alice, bob := identity(t), identity(t)
	_, err := Exchange(relay, alice, bob, "a", 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("expected a pin mismatch, got %v", err)
	}
}

func TestExchangeRefusesARelayWithoutAPin(t *testing.T) {
	relay := startFakeRelay(t)
	relay.Pin = ""
	alice, bob := identity(t), identity(t)
	if _, err := Exchange(relay, alice, bob, "a", 2*time.Second); err == nil {
		t.Fatal("talked to an unpinned relay")
	}
}

func TestExchangeNeedsARegisteredIdentity(t *testing.T) {
	relay := startFakeRelay(t)
	_, stranger, _ := wgnode.GenerateKeyPair()
	bob := identity(t)
	if _, err := Exchange(relay, stranger, bob, "a", time.Second); err == nil {
		t.Fatal("sent a candidate without sealing keys")
	}
}

// The relay sees only pseudonymous IDs and ciphertext.
func TestRelayNeverSeesKeysOrCandidates(t *testing.T) {
	alice, bob := identity(t), identity(t)
	keys, err := derivePair(alice, bob)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{keys.myID, keys.peerID} {
		if strings.Contains(alice, id) || strings.Contains(bob, id) {
			t.Fatal("ID leaks a public key")
		}
	}
	sealed, err := keys.seal("ep1:srflx=198.51.100.7:51820", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "198.51.100.7") {
		t.Fatal("candidate visible in the sealed offer")
	}
	// Bob derives the same pair from his side and opens it.
	back, _ := derivePair(bob, alice)
	if back.myID != keys.peerID || back.peerID != keys.myID {
		t.Fatal("the two sides derived different IDs")
	}
	if got, err := back.open(sealed, time.Now()); err != nil || got != "ep1:srflx=198.51.100.7:51820" {
		t.Fatalf("open: %q %v", got, err)
	}
}

func TestForgedAndStaleOffersAreRejected(t *testing.T) {
	alice, bob, mallory := identity(t), identity(t), identity(t)
	ab, _ := derivePair(alice, bob)
	ba, _ := derivePair(bob, alice)
	mb, _ := derivePair(mallory, bob)
	forged, _ := mb.seal("ep1:srflx=192.0.2.66:51820", time.Now())
	if _, err := ba.open(forged, time.Now()); err == nil {
		t.Fatal("bob accepted an offer sealed by someone else")
	}
	old, _ := ab.seal("ep1:srflx=192.0.2.1:51820", time.Now().Add(-10*time.Minute))
	if _, err := ba.open(old, time.Now()); err == nil {
		t.Fatal("bob accepted a replayed offer")
	}
}

// A relay that injects an unsealed offer gets ignored; the exchange times out.
func TestInjectedOfferIsIgnored(t *testing.T) {
	alice, bob := identity(t), identity(t)
	keys, _ := derivePair(alice, bob)
	l, err := relaysrv.StartLocal(relaysrv.Limits{PortMin: 47100, PortMax: 47101}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	relay := Relay{Label: "test", Address: l.Addr, Pin: l.Pin}
	go func() {
		// A third party that knows Bob's ID (it cannot: here we cheat) still cannot seal for Alice.
		time.Sleep(300 * time.Millisecond)
		conn, err := tls.Dial("tcp", l.Addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test injector
		if err != nil {
			return
		}
		defer conn.Close()
		enc := json.NewEncoder(conn)
		_ = enc.Encode(message{Type: "hello", Pubkey: keys.peerID, To: keys.myID})
		_ = enc.Encode(message{Type: "offer", To: keys.myID, Candidate: "ep1:srflx=192.0.2.66:51820"})
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		time.Sleep(2 * time.Second)
	}()
	if c, err := Exchange(relay, alice, bob, "a", 2*time.Second); err == nil {
		t.Fatalf("accepted an injected candidate %q", c)
	}
}

func TestRequestSessionGrantsABoundedSession(t *testing.T) {
	relay := startFakeRelay(t)
	g, err := RequestSession(relay, 1<<40, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if g.Bytes != 1<<20 || g.Secret == "" || !strings.HasPrefix(g.Endpoint, "127.0.0.1:") {
		t.Fatalf("grant %+v", g)
	}
	st, err := Status(relay, 2*time.Second)
	if err != nil || st.Sessions != 1 || !st.Free {
		t.Fatalf("status %+v %v", st, err)
	}
}
