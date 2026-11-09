package brambled

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/hederapay"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

// acceptAll stands in for the facilitator: it approves and "settles" any
// payment. The signature and transaction rules are covered in hederapay's own
// tests; here the point is what the node does with a relay that charges.
func acceptAll(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var settles atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /supported", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"kinds":[{"x402Version":2,"scheme":"exact","network":"hedera:testnet","extra":{"feePayer":"0.0.7162784"}}]}`)
	})
	mux.HandleFunc("POST /verify", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"isValid":true,"payer":"0.0.5"}`)
	})
	mux.HandleFunc("POST /settle", func(w http.ResponseWriter, _ *http.Request) {
		n := settles.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "payer": "0.0.5", "network": hederapay.NetworkTestnet, "transaction": "0.0.7162784@1.00000000" + string(rune('0'+n))})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &settles
}

func startPaidRelay(t *testing.T, fac string, price int64) (rendezvous.Relay, *relaysrv.Local) {
	t.Helper()
	lo := int(nextRelayPort.Add(100)) - 100
	l, err := relaysrv.StartLocalPaid(relaysrv.Limits{
		SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 16, SessionsPerHour: 100, PortMin: lo, PortMax: lo + 99,
	}, nil, relaysrv.PaywallConfig{
		Facilitator: &relaysrv.Facilitator{BaseURL: fac}, Network: hederapay.NetworkTestnet, Asset: DefaultPayAsset,
		PayTo: "0.0.2", PricePerMiB: price, PublicURL: "https://relay.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return rendezvous.Relay{Label: "paid", Address: l.Addr, Pin: l.Pin}, l
}

func testSigner(t *testing.T) *hederapay.Signer {
	t.Helper()
	key, _, err := hederapay.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := hederapay.NewSigner("0.0.5", key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSizeForKeepsASessionInsideTheCaps(t *testing.T) {
	const MiB = 1 << 20
	cases := []struct {
		name                          string
		bytes, price, session, remain int64
		wantBytes, wantCost           int64
	}{
		{"fits", 4 * MiB, 1000, 500_000, 2_000_000, 4 * MiB, 4000},
		{"trimmed by the session cap", 256 * MiB, 1000, 100_000, 2_000_000, 100 * MiB, 100_000},
		{"trimmed by what is left today", 256 * MiB, 1000, 500_000, 7_500, 7 * MiB, 7000},
		{"one MiB costs too much", 8 * MiB, 1000, 999, 2_000_000, 0, 0},
		{"nothing left today", 8 * MiB, 1000, 500_000, 0, 0, 0},
	}
	for _, c := range cases {
		gotBytes, gotCost := sizeFor(c.bytes, c.price, c.session, c.remain)
		if gotBytes != c.wantBytes || gotCost != c.wantCost {
			t.Errorf("%s: got %d bytes for %d, want %d for %d", c.name, gotBytes, gotCost, c.wantBytes, c.wantCost)
		}
	}
}

func TestANodeWithPaymentsOffSkipsAPaidRelayAndTakesTheFreeOne(t *testing.T) {
	fac, settles := acceptAll(t)
	paid, _ := startPaidRelay(t, fac.URL, 1000)
	free, _ := startRelay(t, "free")
	pool := dataRelayFlag{"paid": paid, "free": free}
	s, err := buyDataRelaySession(freeSessions{}, pool, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if s.relayLabel != "free" || settles.Load() != 0 {
		t.Fatalf("took %q with %d payments", s.relayLabel, settles.Load())
	}
	if _, err := buyDataRelaySession(freeSessions{}, dataRelayFlag{"paid": paid}, 1<<20); err == nil {
		t.Fatal("a node with payments off got a session from a relay that charges")
	}
}

func TestAPayingNodePrefersAFreeRelayAndPaysOnlyWhenItMust(t *testing.T) {
	fac, settles := acceptAll(t)
	paid, _ := startPaidRelay(t, fac.URL, 1000)
	free, _ := startRelay(t, "free")
	src := newPaidSessions(testSigner(t), 0, 0)

	s, err := buyDataRelaySession(src, dataRelayFlag{"paid": paid, "free": free}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if s.relayLabel != "free" || settles.Load() != 0 {
		t.Fatalf("with a free relay up, took %q and paid %d times", s.relayLabel, settles.Load())
	}

	s, err = buyDataRelaySession(src, dataRelayFlag{"paid": paid}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if s.relayLabel != "paid" || settles.Load() != 1 || s.grantedBytes <= 0 {
		t.Fatalf("took %q, %d payments, %d bytes", s.relayLabel, settles.Load(), s.grantedBytes)
	}
	if src.spent != 1000 {
		t.Fatalf("spent %d, want 1000", src.spent)
	}
}

func TestADailyCapStopsPayment(t *testing.T) {
	fac, settles := acceptAll(t)
	paid, _ := startPaidRelay(t, fac.URL, 1000)
	src := newPaidSessions(testSigner(t), 5000, 1500) // one 1 MiB session costs 1000
	if _, err := buyDataRelaySession(src, dataRelayFlag{"paid": paid}, 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := buyDataRelaySession(src, dataRelayFlag{"paid": paid}, 1<<20); err == nil {
		t.Fatal("a second session went past the daily cap")
	}
	if settles.Load() != 1 {
		t.Fatalf("%d payments settled, want 1", settles.Load())
	}
}

func TestANodeRefusesARelayThatChargesInAnotherAsset(t *testing.T) {
	fac, settles := acceptAll(t)
	lo := int(nextRelayPort.Add(100)) - 100
	l, err := relaysrv.StartLocalPaid(relaysrv.Limits{SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 4, SessionsPerHour: 100, PortMin: lo, PortMax: lo + 99},
		nil, relaysrv.PaywallConfig{Facilitator: &relaysrv.Facilitator{BaseURL: fac.URL}, Network: hederapay.NetworkTestnet, Asset: "0.0.999", PayTo: "0.0.2", PricePerMiB: 1, PublicURL: "https://relay.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	r := rendezvous.Relay{Label: "odd", Address: l.Addr, Pin: l.Pin}
	if _, err := buyDataRelaySession(newPaidSessions(testSigner(t), 0, 0), dataRelayFlag{"odd": r}, 1<<20); err == nil {
		t.Fatal("paid a relay that charges in an asset the node does not use")
	}
	if settles.Load() != 0 {
		t.Fatal("a payment settled")
	}
}
