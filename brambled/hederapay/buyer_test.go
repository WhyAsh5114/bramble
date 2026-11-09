package hederapay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeRelay is a relay's x402 route. It checks the layout of what it is paid.
type fakeRelay struct {
	srv      *httptest.Server
	requests atomic.Int32
	paid     atomic.Pointer[Payload]
	asks     []Requirements
	reason   string
	t        *testing.T
}

func newFakeRelay(t *testing.T, asks ...Requirements) *fakeRelay {
	t.Helper()
	f := &fakeRelay{asks: asks, t: t}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.URL.Path != "/x402/session" || r.Method != http.MethodPost || r.URL.Query().Get("bytes") != "524288" {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		sig := r.Header.Get(headerSignature)
		if sig == "" {
			f.require(w, "")
			return
		}
		var p Payload
		if err := decodeHeader(sig, &p); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		f.paid.Store(&p)
		if f.reason != "" {
			f.require(w, f.reason)
			return
		}
		_ = json.NewEncoder(w).Encode(Purchase{Token: "tok.sig", Bytes: 524288, ExpiresAt: 1, Transaction: "0.0.7000@1.1"})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelay) require(w http.ResponseWriter, reason string) {
	enc, _ := encodeHeader(Required{X402Version: 2, Error: reason, Resource: Resource{URL: "https://relay/x402/session"}, Accepts: f.asks})
	w.Header().Set(headerRequired, enc)
	w.WriteHeader(http.StatusPaymentRequired)
}

func testBuyer(t *testing.T, maxAmount int64) *Buyer {
	s, _ := testSigner(t)
	return &Buyer{Signer: s, MaxAmount: map[string]int64{testAsset: maxAmount}}
}

func TestBuySessionPaysTheRelaysPriceAndReturnsTheToken(t *testing.T) {
	f := newFakeRelay(t, testRequirements())
	got, err := testBuyer(t, 1000).BuySession(context.Background(), f.srv.Client(), f.srv.URL, 524288)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "tok.sig" || got.Bytes != 524288 {
		t.Fatalf("purchase %+v", got)
	}
	if n := f.requests.Load(); n != 2 {
		t.Fatalf("%d requests, expected the 402 and the paid retry", n)
	}
	p := f.paid.Load()
	if p == nil || p.X402Version != 2 || p.Accepted.Amount != "500" || p.Accepted.PayTo != testPayTo || p.Resource.URL == "" {
		t.Fatalf("payload %+v", p)
	}
	tx := decode(t, p.Payload.Transaction)
	if tx.GetTransactionID().AccountID.String() != testFeePayer {
		t.Fatal("the paid transaction is not owned by the fee payer")
	}
}

func TestBuySessionNeverPaysOverTheCap(t *testing.T) {
	over := testRequirements()
	over.Amount = "1001"
	f := newFakeRelay(t, over)
	_, err := testBuyer(t, 1000).BuySession(context.Background(), f.srv.Client(), f.srv.URL, 524288)
	if err == nil || !strings.Contains(err.Error(), "spend cap") {
		t.Fatalf("error %v", err)
	}
	if f.requests.Load() != 1 || f.paid.Load() != nil {
		t.Fatal("the node signed or sent a payment above its cap")
	}
}

func TestBuySessionRefusesAssetsAndNetworksItDoesNotKnow(t *testing.T) {
	other := testRequirements()
	other.Asset = "0.0.5555"
	mainnet := testRequirements()
	mainnet.Network = "hedera:mainnet"
	noFee := testRequirements()
	noFee.Extra = nil
	other2 := testRequirements()
	other2.Scheme = "upto"
	for name, ask := range map[string]Requirements{"unlisted asset": other, "mainnet": mainnet, "no fee payer": noFee, "another scheme": other2} {
		f := newFakeRelay(t, ask)
		if _, err := testBuyer(t, 1000).BuySession(context.Background(), f.srv.Client(), f.srv.URL, 524288); err == nil || !strings.Contains(err.Error(), "accepts no payment") {
			t.Fatalf("%s: error %v", name, err)
		}
		if f.paid.Load() != nil {
			t.Fatalf("%s: a payment was sent", name)
		}
	}
}

func TestBuySessionReportsRefusalAsACodeNeverUpstreamText(t *testing.T) {
	f := newFakeRelay(t, testRequirements())
	f.reason = "insufficient_funds"
	_, err := testBuyer(t, 1000).BuySession(context.Background(), f.srv.Client(), f.srv.URL, 524288)
	var pe *PaymentError
	if !asPaymentError(err, &pe) || pe.Reason != "insufficient_funds" {
		t.Fatalf("error %v", err)
	}
	f.reason = "<script>alert(1)</script> with the signed transaction inside"
	_, err = testBuyer(t, 1000).BuySession(context.Background(), f.srv.Client(), f.srv.URL, 524288)
	if !asPaymentError(err, &pe) || pe.Reason != "payment_failed" {
		t.Fatalf("hostile text was passed on: %v", err)
	}
}

func asPaymentError(err error, target **PaymentError) bool {
	pe, ok := err.(*PaymentError) //nolint:errorlint // PaymentError is returned unwrapped
	if ok {
		*target = pe
	}
	return ok
}

func TestBuySessionOnlyOverHTTPS(t *testing.T) {
	if _, err := testBuyer(t, 1000).BuySession(context.Background(), http.DefaultClient, "http://relay.example", 524288); err == nil {
		t.Fatal("paid over plain http")
	}
	if _, err := testBuyer(t, 1000).BuySession(context.Background(), http.DefaultClient, "relay.example", 524288); err == nil {
		t.Fatal("accepted a URL with no scheme")
	}
}

func TestBuySessionExpectsA402First(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	if _, err := testBuyer(t, 1000).BuySession(context.Background(), srv.Client(), srv.URL, 524288); err == nil {
		t.Fatal("a relay that asked for nothing was paid")
	}
}
