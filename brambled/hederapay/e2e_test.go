package hederapay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

// specFacilitator applies the verification rules of the x402 Hedera exact
// scheme to the transaction it is given, then pretends to settle it. It
// refuses a transaction ID it has settled before. It stands in for Blocky402
// so the whole flow runs offline; it does not prove the live facilitator
// accepts the same bytes.
type specFacilitator struct {
	srv     *httptest.Server
	payer   hiero.PublicKey
	mu      sync.Mutex
	settled map[string]bool
	verify  int
	settle  int
}

type fReq struct {
	PaymentPayload      Payload      `json:"paymentPayload"`
	PaymentRequirements Requirements `json:"paymentRequirements"`
}

func newSpecFacilitator(t *testing.T, payer hiero.PublicKey) *specFacilitator {
	t.Helper()
	f := &specFacilitator{payer: payer, settled: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /supported", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"kinds":[{"x402Version":2,"scheme":"exact","network":"hedera:testnet","extra":{"feePayer":"`+testFeePayer+`"}}]}`)
	})
	mux.HandleFunc("POST /verify", func(w http.ResponseWriter, r *http.Request) {
		var in fReq
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.verify++
		f.mu.Unlock()
		reason := f.check(in)
		_ = json.NewEncoder(w).Encode(map[string]any{"isValid": reason == "", "invalidReason": reason, "payer": testPayer})
	})
	mux.HandleFunc("POST /settle", func(w http.ResponseWriter, r *http.Request) {
		var in fReq
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.settle++
		reason := f.check(in)
		var id string
		if reason == "" {
			id = decode(t, in.PaymentPayload.Payload.Transaction).GetTransactionID().String()
			if f.settled[id] {
				reason = "invalid_transaction_state"
			}
		}
		if reason != "" {
			_ = json.NewEncoder(w).Encode(Settlement{Success: false, ErrorReason: reason, Network: NetworkTestnet})
			return
		}
		f.settled[id] = true
		_ = json.NewEncoder(w).Encode(Settlement{Success: true, Payer: testPayer, Transaction: id, Network: NetworkTestnet})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *specFacilitator) check(in fReq) string {
	req := in.PaymentRequirements
	tx := decode(&testing.T{}, in.PaymentPayload.Payload.Transaction)
	if id := tx.GetTransactionID(); id.AccountID == nil || id.AccountID.String() != req.Extra["feePayer"] {
		return "invalid_exact_hedera_payload_fee_payer"
	}
	if len(tx.GetHbarTransfers()) != 0 {
		return "invalid_exact_hedera_payload_unexpected_hbar"
	}
	got := map[string]int64{}
	var net int64
	for token, list := range tx.GetTokenTransfers() {
		if token.String() != req.Asset {
			return "invalid_exact_hedera_payload_asset"
		}
		for _, e := range list {
			got[e.AccountID.String()] += e.Amount
			net += e.Amount
		}
	}
	if net != 0 || got[req.Extra["feePayer"]] < 0 || len(got) != 2 {
		return "invalid_exact_hedera_payload_transfers"
	}
	if got[req.PayTo] != parseAmount(req.Amount) {
		return "invalid_exact_hedera_payload_amount"
	}
	if !f.payer.VerifyTransaction(tx) {
		return "invalid_exact_hedera_payload_signature_invalid"
	}
	return ""
}

func parseAmount(s string) int64 {
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

func startPaidLocal(t *testing.T, f *specFacilitator, pricePerMiB int64) *relaysrv.Local {
	t.Helper()
	l, err := relaysrv.StartLocalPaid(relaysrv.Limits{
		SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 16, SessionsPerHour: 100, PortMin: 52000, PortMax: 52099,
	}, nil, relaysrv.PaywallConfig{
		Facilitator: &relaysrv.Facilitator{BaseURL: f.srv.URL}, Network: NetworkTestnet, Asset: testAsset,
		PayTo: testPayTo, PricePerMiB: pricePerMiB, PublicURL: "https://relay.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l
}

func TestPayingARealRelayBuysOneSession(t *testing.T) {
	key, err := hiero.PrivateKeyGenerateEcdsa()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner(testPayer, key.StringRaw())
	if err != nil {
		t.Fatal(err)
	}
	f := newSpecFacilitator(t, key.PublicKey())
	l := startPaidLocal(t, f, 1000)
	relay := rendezvous.Relay{Label: "paid", Address: l.Addr, Pin: l.Pin}
	buyer := &Buyer{Signer: signer, MaxAmount: map[string]int64{testAsset: 1000}}

	// The relay advertises its price in its status.
	st, err := rendezvous.Status(relay, 5*time.Second)
	if err != nil || st.Free || st.Paid == nil || st.Paid.PricePerMiB != 1000 || st.Paid.PayTo != testPayTo {
		t.Fatalf("status %+v err %v", st, err)
	}

	// A free session is refused.
	if _, err := rendezvous.RequestSession(relay, 4096, 5*time.Second); err == nil {
		t.Fatal("a paid relay gave a session without payment")
	}

	ctx := context.Background()
	hc := rendezvous.HTTPClient(relay, 10*time.Second)
	p, err := buyer.BuySession(ctx, hc, "https://"+l.Addr, 524288)
	if err != nil {
		t.Fatal(err)
	}
	if p.Bytes != 524288 || p.Transaction == "" {
		t.Fatalf("purchase %+v", p)
	}

	grant, err := rendezvous.RequestSessionWithToken(relay, 0, p.Token, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Bytes != 524288 || grant.Endpoint == "" {
		t.Fatalf("grant %+v", grant)
	}
	if _, err := rendezvous.RequestSessionWithToken(relay, 0, p.Token, 5*time.Second); err == nil {
		t.Fatal("the token opened a second session")
	}
	if f.verify != 1 || f.settle != 1 {
		t.Fatalf("verify %d settle %d", f.verify, f.settle)
	}
}

func TestARealRelayAboveTheCapIsNeverPaid(t *testing.T) {
	key, _ := hiero.PrivateKeyGenerateEcdsa()
	signer, _ := NewSigner(testPayer, key.StringRaw())
	f := newSpecFacilitator(t, key.PublicKey())
	l := startPaidLocal(t, f, 5000) // 2500 for half a MiB
	relay := rendezvous.Relay{Label: "dear", Address: l.Addr, Pin: l.Pin}
	buyer := &Buyer{Signer: signer, MaxAmount: map[string]int64{testAsset: 1000}}
	if _, err := buyer.BuySession(context.Background(), rendezvous.HTTPClient(relay, 10*time.Second), "https://"+l.Addr, 524288); err == nil {
		t.Fatal("paid above the cap")
	}
	if f.verify != 0 || f.settle != 0 {
		t.Fatal("a payment reached the facilitator")
	}
}

func TestASessionPaidWithTheWrongKeyIsRefusedBySettlement(t *testing.T) {
	signing, _ := hiero.PrivateKeyGenerateEcdsa()
	account, _ := hiero.PrivateKeyGenerateEcdsa() // the key the account really has
	signer, _ := NewSigner(testPayer, signing.StringRaw())
	f := newSpecFacilitator(t, account.PublicKey())
	l := startPaidLocal(t, f, 1000)
	relay := rendezvous.Relay{Label: "paid", Address: l.Addr, Pin: l.Pin}
	buyer := &Buyer{Signer: signer, MaxAmount: map[string]int64{testAsset: 1000}}
	_, err := buyer.BuySession(context.Background(), rendezvous.HTTPClient(relay, 10*time.Second), "https://"+l.Addr, 524288)
	if err == nil {
		t.Fatal("a payment signed by the wrong key bought a session")
	}
	if f.settle != 0 {
		t.Fatal("settled a payment that failed verification")
	}
	if _, err := rendezvous.RequestSession(relay, 4096, 5*time.Second); err == nil {
		t.Fatal("a session opened without a paid token")
	}
}
