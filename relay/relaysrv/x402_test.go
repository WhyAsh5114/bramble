package relaysrv

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeFacilitator stands in for Blocky402. It records what the relay sent.
type fakeFacilitator struct {
	srv        *httptest.Server
	verifyOK   atomic.Bool
	settleOK   atomic.Bool
	verifyReq  atomic.Pointer[facilitatorRequest]
	settleReq  atomic.Pointer[facilitatorRequest]
	verifyCall atomic.Int32
	settleCall atomic.Int32
	reason     atomic.Value
}

func newFakeFacilitator(t *testing.T) *fakeFacilitator {
	t.Helper()
	f := &fakeFacilitator{}
	f.verifyOK.Store(true)
	f.settleOK.Store(true)
	f.reason.Store("")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /supported", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"kinds":[{"x402Version":2,"scheme":"exact","network":"eip155:80002"},`+
			`{"x402Version":2,"scheme":"exact","network":"hedera:testnet","extra":{"feePayer":"0.0.7000"}}]}`)
	})
	mux.HandleFunc("POST /verify", func(w http.ResponseWriter, r *http.Request) {
		f.verifyCall.Add(1)
		var in facilitatorRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.verifyReq.Store(&in)
		_ = json.NewEncoder(w).Encode(verifyResponse{IsValid: f.verifyOK.Load(), InvalidReason: f.reason.Load().(string), Payer: "0.0.5"})
	})
	mux.HandleFunc("POST /settle", func(w http.ResponseWriter, r *http.Request) {
		f.settleCall.Add(1)
		var in facilitatorRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.settleReq.Store(&in)
		ok := f.settleOK.Load()
		out := SettlementResponse{Success: ok, Payer: "0.0.5", Network: "hedera:testnet"}
		if ok {
			out.Transaction = "0.0.7000@1700000000.000000001"
		} else {
			out.ErrorReason = f.reason.Load().(string)
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

const testPricePerMiB = 1000

func startPaidRelay(t *testing.T, f *fakeFacilitator) (addr string, pw *Paywall) {
	t.Helper()
	dir := t.TempDir()
	key, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := TLSConfig(key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	pw, err = NewPaywall(PaywallConfig{
		Facilitator: &Facilitator{BaseURL: f.srv.URL}, Network: "hedera:testnet", Asset: "0.0.429274",
		PayTo: "0.0.9000", PricePerMiB: testPricePerMiB, MaxSessionBytes: defaultLimits.SessionBytes,
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(NewDataRelay("127.0.0.1", defaultLimits, NewBudget("", 0, 0)), "127.0.0.1", "test")
	srv.SetPaywall(pw)
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), pw
}

func httpsClient() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true}, //nolint:gosec // tests do not check the pin
	}
}

func postSession(t *testing.T, addr, query string, payload *PaymentPayload) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://"+addr+"/x402/session"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		enc, err := encodeHeader(payload)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(headerSignature, enc)
	}
	resp, err := httpsClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func requiredOf(t *testing.T, resp *http.Response) PaymentRequired {
	t.Helper()
	var pr PaymentRequired
	if err := decodeHeader(resp.Header.Get(headerRequired), &pr); err != nil {
		t.Fatalf("no readable %s header: %v", headerRequired, err)
	}
	return pr
}

func payloadFor(req PaymentRequirements) *PaymentPayload {
	p := &PaymentPayload{X402Version: x402Version, Accepted: req}
	p.Payload.Transaction = "AAAA"
	return p
}

func TestPaidSessionAnswers402WithRelayPrice(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	resp := postSession(t, addr, "?bytes=524288", nil) // half a MiB
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status %d", resp.StatusCode)
	}
	pr := requiredOf(t, resp)
	if pr.X402Version != 2 || len(pr.Accepts) != 1 {
		t.Fatalf("unexpected requirements %+v", pr)
	}
	got := pr.Accepts[0]
	if got.Scheme != "exact" || got.Network != "hedera:testnet" || got.Asset != "0.0.429274" || got.PayTo != "0.0.9000" {
		t.Fatalf("wrong requirements %+v", got)
	}
	if got.Amount != "500" || got.Extra["feePayer"] != "0.0.7000" {
		t.Fatalf("price or fee payer wrong: amount %s feePayer %s", got.Amount, got.Extra["feePayer"])
	}
	if f.verifyCall.Load() != 0 || f.settleCall.Load() != 0 {
		t.Fatal("the facilitator was called before any payment")
	}
}

func TestPaidSessionMintsOneSingleUseToken(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	pr := requiredOf(t, postSession(t, addr, "?bytes=524288", nil))

	resp := postSession(t, addr, "?bytes=524288", payloadFor(pr.Accepts[0]))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paid request got %d", resp.StatusCode)
	}
	var reply sessionTokenReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.Token == "" || reply.Bytes != 524288 || reply.Transaction == "" {
		t.Fatalf("unexpected reply %+v", reply)
	}
	var settled SettlementResponse
	if err := decodeHeader(resp.Header.Get(headerResponse), &settled); err != nil || !settled.Success {
		t.Fatalf("missing settlement header: %v %+v", err, settled)
	}
	if f.verifyCall.Load() != 1 || f.settleCall.Load() != 1 {
		t.Fatalf("verify %d settle %d", f.verifyCall.Load(), f.settleCall.Load())
	}

	c := dial(t, addr)
	c.send(t, message{Type: "session", Token: reply.Token})
	m := c.recv(t)
	if m.Type != "session" || m.Session == nil || m.Session.Bytes != 524288 {
		t.Fatalf("session with a paid token: %+v", m)
	}
	c.send(t, message{Type: "session", Token: reply.Token})
	if m := c.recv(t); m.Type != "error" || !strings.Contains(m.Message, "payment required") {
		t.Fatalf("a spent token opened a second session: %+v", m)
	}
}

func TestSessionWithoutTokenIsRefusedOnPaidRelay(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	c := dial(t, addr)
	c.send(t, message{Type: "session", Bytes: 1024})
	if m := c.recv(t); m.Type != "error" || !strings.Contains(m.Message, "payment required") {
		t.Fatalf("a free session opened on a paid relay: %+v", m)
	}
	c.send(t, message{Type: "session", Token: "garbage.token"})
	if m := c.recv(t); m.Type != "error" {
		t.Fatalf("a forged token opened a session: %+v", m)
	}
}

func TestRelayIgnoresTheClientsAcceptedCopy(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	pr := requiredOf(t, postSession(t, addr, "?bytes=524288", nil))
	cheat := pr.Accepts[0]
	cheat.Amount = "1"
	resp := postSession(t, addr, "?bytes=524288", payloadFor(cheat))
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("a cheaper accepted copy got %d", resp.StatusCode)
	}
	if got := requiredOf(t, resp); got.Error == "" || got.Accepts[0].Amount != "500" {
		t.Fatalf("the relay did not restate its own price: %+v", got)
	}
	redirect := pr.Accepts[0]
	redirect.PayTo = "0.0.1"
	if resp := postSession(t, addr, "?bytes=524288", payloadFor(redirect)); resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("a redirected payTo got %d", resp.StatusCode)
	}
	if f.verifyCall.Load() != 0 || f.settleCall.Load() != 0 {
		t.Fatal("the facilitator saw a payment the relay had not asked for")
	}
}

func TestFacilitatorOnlySeesTheRelaysRequirements(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	pr := requiredOf(t, postSession(t, addr, "?bytes=524288", nil))
	if resp := postSession(t, addr, "?bytes=524288", payloadFor(pr.Accepts[0])); resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for name, in := range map[string]*facilitatorRequest{"verify": f.verifyReq.Load(), "settle": f.settleReq.Load()} {
		if in == nil || in.PaymentRequirements.Amount != "500" || in.PaymentRequirements.PayTo != "0.0.9000" || in.X402Version != 2 {
			t.Fatalf("%s got %+v", name, in)
		}
		if in.PaymentPayload.Payload.Transaction != "AAAA" {
			t.Fatalf("%s lost the transaction", name)
		}
	}
}

func TestFailedVerifyMintsNothingAndSkipsSettle(t *testing.T) {
	f := newFakeFacilitator(t)
	f.verifyOK.Store(false)
	f.reason.Store("insufficient_funds")
	addr, _ := startPaidRelay(t, f)
	pr := requiredOf(t, postSession(t, addr, "?bytes=524288", nil))
	resp := postSession(t, addr, "?bytes=524288", payloadFor(pr.Accepts[0]))
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := requiredOf(t, resp); got.Error != "insufficient_funds" {
		t.Fatalf("reason %q", got.Error)
	}
	if f.settleCall.Load() != 0 {
		t.Fatal("settled a payment that failed verification")
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "token") {
		t.Fatal("a failed payment returned a token")
	}
}

func TestFailedSettleMintsNoToken(t *testing.T) {
	f := newFakeFacilitator(t)
	f.settleOK.Store(false)
	f.reason.Store("settlement_failed")
	addr, _ := startPaidRelay(t, f)
	pr := requiredOf(t, postSession(t, addr, "?bytes=524288", nil))
	resp := postSession(t, addr, "?bytes=524288", payloadFor(pr.Accepts[0]))
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var settled SettlementResponse
	if err := decodeHeader(resp.Header.Get(headerResponse), &settled); err != nil || settled.Success {
		t.Fatalf("expected a failed settlement header: %v %+v", err, settled)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "token") {
		t.Fatal("a failed settlement returned a token")
	}
}

func TestMalformedPaymentIsABadRequest(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	req, _ := http.NewRequest(http.MethodPost, "https://"+addr+"/x402/session", nil)
	req.Header.Set(headerSignature, "%%%not-base64%%%")
	resp, err := httpsClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestUpstreamTextNeverReachesTheClient(t *testing.T) {
	for _, in := range []string{"", "Some <b>HTML</b> from upstream", strings.Repeat("a", 200), "has space"} {
		if got := safeReason(in); got != "payment_failed" {
			t.Fatalf("safeReason(%q) = %q", in, got)
		}
	}
	if got := safeReason("insufficient_funds"); got != "insufficient_funds" {
		t.Fatalf("a plain code was changed: %q", got)
	}
}

func TestTokensExpireAndCannotBeForged(t *testing.T) {
	f := newFakeFacilitator(t)
	_, pw := startPaidRelay(t, f)
	token, _, err := pw.mint(4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Redeem(token + "x"); err == nil {
		t.Fatal("a token with a changed signature was accepted")
	}
	body, sig, _ := strings.Cut(token, ".")
	if _, err := pw.Redeem(body + "A." + sig); err == nil {
		t.Fatal("a token with a changed body was accepted")
	}
	pw.now = func() time.Time { return time.Now().Add(2 * tokenTTL) }
	if _, err := pw.Redeem(token); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

func TestPriceRoundsUpAndIsNeverZero(t *testing.T) {
	f := newFakeFacilitator(t)
	_, pw := startPaidRelay(t, f)
	for bytes, want := range map[int64]int64{1: 1, 1024: 1, bytesPerMiB: testPricePerMiB, bytesPerMiB + 1: testPricePerMiB + 1, 524288: 500} {
		if got := pw.Amount(bytes); got != want {
			t.Fatalf("Amount(%d) = %d, want %d", bytes, got, want)
		}
	}
}

func TestStatusAdvertisesThePrice(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	c := dial(t, addr)
	c.send(t, message{Type: "status"})
	m := c.recv(t)
	if m.Status == nil || m.Status.Free || m.Status.Paid == nil || m.Status.Paid.PricePerMiB != testPricePerMiB || m.Status.Paid.PayTo != "0.0.9000" {
		t.Fatalf("status %+v", m.Status)
	}
}

func TestFreeRelayIsUnchanged(t *testing.T) {
	addr, _ := startRelay(t, defaultLimits, nil)
	c := dial(t, addr)
	c.send(t, message{Type: "session", Bytes: 1024})
	if m := c.recv(t); m.Type != "session" || m.Session == nil {
		t.Fatalf("a free relay refused a free session: %+v", m)
	}
	c.send(t, message{Type: "status"})
	if m := c.recv(t); m.Status == nil || !m.Status.Free || m.Status.Paid != nil {
		t.Fatalf("a free relay reports %+v", m.Status)
	}
	// A free relay has no HTTP routes; a request line is treated as a bad
	// rendezvous message and the connection closes.
	req, _ := http.NewRequest(http.MethodPost, "https://"+addr+"/x402/session", nil)
	if resp, err := httpsClient().Do(req); err == nil {
		resp.Body.Close()
		t.Fatalf("a free relay served HTTP: %d", resp.StatusCode)
	}
}

func TestRendezvousStillWorksOnAPaidRelay(t *testing.T) {
	f := newFakeFacilitator(t)
	addr, _ := startPaidRelay(t, f)
	a, b := dial(t, addr), dial(t, addr)
	a.send(t, message{Type: "hello", Pubkey: "idA", To: "idB"})
	b.send(t, message{Type: "hello", Pubkey: "idB", To: "idA"})
	time.Sleep(50 * time.Millisecond)
	a.send(t, message{Type: "offer", To: "idB", Candidate: "sealed-blob"})
	if m := b.recv(t); m.Type != "offer" || m.Candidate != "sealed-blob" {
		t.Fatalf("b got %+v", m)
	}
}
