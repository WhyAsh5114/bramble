package sidecar

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func slowSidecar(t *testing.T, delay time.Duration, body string) *Manager {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // lets the server notice a client that gives up
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing test server port: %v", err)
	}
	return &Manager{Port: port}
}

// Tor-routed ENS reads and payments take longer than the 8 s flat timeout
// these calls used to share, so a valid answer arriving after 9 s must be
// accepted (ETH-121 review, P1).
const torLatency = 9 * time.Second

func TestResolveDevice_AcceptsAnswerSlowerThanOldFlatTimeout(t *testing.T) {
	t.Parallel()
	m := slowSidecar(t, torLatency, `{"fullname":"device.t.eth","pubkey":"ab","status":1,"expiry":"1","tokenId":"1"}`)

	record, err := m.ResolveDevice("device")
	if err != nil {
		t.Fatalf("ResolveDevice: %v", err)
	}
	if record.Fullname != "device.t.eth" {
		t.Fatalf("Fullname = %q", record.Fullname)
	}
}

func TestRendezvousToken_AcceptsPaymentSlowerThanOldFlatTimeout(t *testing.T) {
	t.Parallel()
	m := slowSidecar(t, torLatency, `{"token":"tok","expiresAt":1}`)

	resp, err := m.RendezvousToken("http://relay.invalid")
	if err != nil {
		t.Fatalf("RendezvousToken: %v", err)
	}
	if resp.Token != "tok" {
		t.Fatalf("Token = %q", resp.Token)
	}
}

func TestDataRelaySession_AcceptsPaymentSlowerThanOldFlatTimeout(t *testing.T) {
	t.Parallel()
	m := slowSidecar(t, torLatency, `{"sessionId":"s","port":1,"expiresAt":1}`)

	resp, err := m.DataRelaySession("http://relay.invalid", 1024)
	if err != nil {
		t.Fatalf("DataRelaySession: %v", err)
	}
	if resp.SessionID != "s" {
		t.Fatalf("SessionID = %q", resp.SessionID)
	}
}

// The budget is still a bound: a sidecar that never answers must not stall
// the caller forever. Not parallel, because it swaps the package budgets.
func TestCalls_GiveUpOnceTheirBudgetIsSpent(t *testing.T) {
	oldENS, oldPay := ensReadBudget, paymentBudget
	ensReadBudget, paymentBudget = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { ensReadBudget, paymentBudget = oldENS, oldPay })

	m := slowSidecar(t, time.Minute, `{}`)
	calls := map[string]func() error{
		"ResolveDevice": func() error { _, err := m.ResolveDevice("device"); return err },
		"Relays":        func() error { _, err := m.Relays(); return err },
		"ListDevices":   func() error { _, err := m.ListDevices(); return err },
		"RendezvousToken": func() error {
			_, err := m.RendezvousToken("http://relay.invalid")
			return err
		},
		"DataRelaySession": func() error {
			_, err := m.DataRelaySession("http://relay.invalid", 1)
			return err
		},
	}
	for name, call := range calls {
		start := time.Now()
		err := call()
		if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
			t.Errorf("%s: err = %v, want deadline exceeded", name, err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("%s: took %v, want about the 200ms budget", name, elapsed)
		}
	}
}
