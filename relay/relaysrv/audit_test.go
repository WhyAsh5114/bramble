package relaysrv

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sink struct {
	mu  sync.Mutex
	got []Receipt
}

func (s *sink) Submit(r Receipt) {
	s.mu.Lock()
	s.got = append(s.got, r)
	s.mu.Unlock()
}

func startAuditedRelay(t *testing.T, f *fakeFacilitator, audit ReceiptSink) string {
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
	pw, err := NewPaywall(PaywallConfig{
		Facilitator: &Facilitator{BaseURL: f.srv.URL}, Network: "hedera:testnet", Asset: "0.0.429274",
		PayTo: "0.0.9000", PricePerMiB: testPricePerMiB, MaxSessionBytes: defaultLimits.SessionBytes,
		Audit: audit, AuditTopic: "0.0.5555",
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(NewDataRelay("127.0.0.1", defaultLimits, NewBudget("", 0, 0)), "127.0.0.1", "test")
	srv.SetPaywall(pw)
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String()
}

func TestASoldSessionLeavesOneReceiptAndAFailedPaymentLeavesNone(t *testing.T) {
	f := newFakeFacilitator(t)
	s := &sink{}
	addr := startAuditedRelay(t, f, s)
	pr := requiredOf(t, postSession(t, addr, "?bytes=524288", nil))

	f.verifyOK.Store(false)
	postSession(t, addr, "?bytes=524288", payloadFor(pr.Accepts[0]))
	f.verifyOK.Store(true)
	if len(s.got) != 0 {
		t.Fatalf("a refused payment left a receipt: %+v", s.got)
	}

	resp := postSession(t, addr, "?bytes=524288", payloadFor(pr.Accepts[0]))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paid request got %d", resp.StatusCode)
	}
	if len(s.got) != 1 {
		t.Fatalf("%d receipts, want 1", len(s.got))
	}
	r := s.got[0]
	if r.Bytes != 524288 || r.Amount == "" || r.Asset != "0.0.429274" || r.Network != "hedera:testnet" || r.Transaction == "" {
		t.Fatalf("receipt %+v", r)
	}
}

func TestStatusNamesTheAuditTopic(t *testing.T) {
	addr := startAuditedRelay(t, newFakeFacilitator(t), &sink{})
	c := dial(t, addr)
	c.send(t, message{Type: "status"})
	if m := c.recv(t); m.Status == nil || m.Status.Paid == nil || m.Status.Paid.AuditTopic != "0.0.5555" {
		t.Fatalf("status %+v", m.Status)
	}
}

func TestAReceiptHoldsNoPayerOrPeer(t *testing.T) {
	raw, err := encodeReceipt(Receipt{Transaction: "0.0.7@1.2", Bytes: 10, Amount: "5", Asset: "0.0.1", Network: "hedera:testnet", At: time.Unix(1700000000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"v": true, "kind": true, "tx": true, "bytes": true, "amount": true, "asset": true, "network": true, "at": true}
	for k := range m {
		if !want[k] {
			t.Errorf("unexpected field %q in a receipt", k)
		}
	}
	if m["at"] != float64(1700000000) || m["kind"] != "relay-session" || m["v"] != float64(1) {
		t.Fatalf("receipt %s", raw)
	}
}

func TestTheAuditorRetriesThenGivesUpWithoutBlocking(t *testing.T) {
	var calls atomic.Int32
	a := newAuditor("0.0.5555", func(ctx context.Context, msg []byte) error {
		if calls.Add(1) < 2 {
			return errors.New("busy")
		}
		if !strings.Contains(string(msg), `"relay-session"`) {
			t.Errorf("message %s", msg)
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()
	a.Submit(Receipt{Transaction: "x", At: time.Now()})
	deadline := time.Now().Add(10 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done
	if calls.Load() != 2 {
		t.Fatalf("%d submit calls, want a retry then success", calls.Load())
	}
}

func TestAFullQueueDropsInsteadOfBlocking(t *testing.T) {
	a := newAuditor("0.0.1", func(context.Context, []byte) error { return nil }) // never run: nothing drains
	finished := make(chan struct{})
	go func() {
		for i := 0; i < auditQueue*2; i++ {
			a.Submit(Receipt{Transaction: "x"})
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Submit blocked on a full queue")
	}
}
