package daemon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func payDaemon(t *testing.T, mirror string) *Daemon {
	t.Helper()
	d := &Daemon{Store: &Store{Dir: t.TempDir()}, Mirror: mirror,
		activateFn: func(context.Context, string, string) error {
			t.Error("sent a transaction to the real network")
			return nil
		}}
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPayIsOffUntilTheUserTurnsItOn(t *testing.T) {
	d := payDaemon(t, "http://127.0.0.1:1")
	if info := d.Pay(); info.Enabled || info.EVMAddress != "" {
		t.Fatalf("payments are on by default: %+v", info)
	}
	if _, ok, _ := d.Store.LoadHederaKey(); ok {
		t.Fatal("a Hedera key exists before the user opted in")
	}
	if args := d.payArgsLocked(); len(args) != 0 {
		t.Fatalf("payment flags with payments off: %v", args)
	}
}

func TestTurningPayOnMakesAKeyOnlyThisDeviceHolds(t *testing.T) {
	var hits atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound) // not funded yet
	}))
	defer mirror.Close()
	d := payDaemon(t, mirror.URL)
	if err := d.SetPay(true, 0, 0); err != nil {
		t.Fatal(err)
	}
	info := d.Pay()
	if !info.Enabled || !strings.HasPrefix(info.EVMAddress, "0x") || len(info.EVMAddress) != 42 {
		t.Fatalf("pay info %+v", info)
	}
	fi, err := os.Stat(d.Store.KeyPath())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file %v %v", fi, err)
	}
	// The key is never part of what the API returns.
	raw, _ := os.ReadFile(d.Store.KeyPath())
	key := d.hedera.PrivateKeyHex
	if key == "" || strings.Contains(strings.ToLower(info.EVMAddress+info.Account+info.Error+info.KeyFile), key) || !strings.Contains(string(raw), key) {
		t.Fatal("key handling is wrong")
	}
	// Unfunded: no account, so the node gets no payment flags yet.
	d.refreshPay(context.Background())
	if hits.Load() == 0 || d.Pay().Account != "" {
		t.Fatalf("hits=%d account=%q", hits.Load(), d.Pay().Account)
	}
	d.mu.Lock()
	args := d.payArgsLocked()
	d.mu.Unlock()
	if len(args) != 0 {
		t.Fatalf("payment flags before the account exists: %v", args)
	}
	// A second enable keeps the same key.
	first := d.hedera.EVMAddress
	if err := d.SetPay(true, 100, 200); err != nil || d.hedera.EVMAddress != first {
		t.Fatalf("key changed or error: %v", err)
	}
}

func TestAFundedAccountTurnsPaymentOnInTheNode(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tokens") {
			_, _ = io.WriteString(w, `{"tokens":[{"token_id":"0.0.429274"}]}`)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/accounts/0x") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"account":"0.0.4242","key":{"key":"02ab"},"balance":{"balance":99000,"tokens":[{"token_id":"0.0.429274","balance":2500000},{"token_id":"0.0.1","balance":7}]}}`)
	}))
	defer mirror.Close()
	d := payDaemon(t, mirror.URL)
	if err := d.SetPay(true, 300_000, 900_000); err != nil {
		t.Fatal(err)
	}
	d.refreshPay(context.Background())
	info := d.Pay()
	if info.Account != "0.0.4242" || info.USDC != 2_500_000 || info.HBAR != 99000 || info.MaxSession != 300_000 || info.MaxDay != 900_000 {
		t.Fatalf("pay info %+v", info)
	}
	d.mu.Lock()
	args := strings.Join(d.payArgsLocked(), " ")
	d.mu.Unlock()
	for _, want := range []string{"-hedera-account 0.0.4242", "-pay-max-session 300000", "-pay-max-day 900000"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q lack %q", strings.ReplaceAll(args, d.hedera.PrivateKeyHex, "<key>"), want)
		}
	}
	// Turning it off removes the flags again.
	if err := d.SetPay(false, 0, 0); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	off := d.payArgsLocked()
	d.mu.Unlock()
	if len(off) != 0 {
		t.Fatalf("flags after turning payments off: %d", len(off))
	}
}

func TestPayCapsAreChecked(t *testing.T) {
	d := payDaemon(t, "http://127.0.0.1:1")
	if err := d.SetPay(true, -1, 0); err == nil {
		t.Fatal("a negative cap was accepted")
	}
	if err := d.SetPay(true, 900, 100); err == nil {
		t.Fatal("a per-session cap above the daily cap was accepted")
	}
}

func TestAHollowAccountGetsItsFirstTransactionOnce(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"account":"0.0.4242","key":null,"balance":{"balance":99000,"tokens":[]}}`)
	}))
	defer mirror.Close()
	d := payDaemon(t, mirror.URL)
	var calls int
	var account string
	d.activateFn = func(_ context.Context, a, key string) error {
		calls++
		account = a
		if key != d.hedera.PrivateKeyHex {
			t.Error("activation used a different key")
		}
		return nil
	}
	if err := d.SetPay(true, 0, 0); err != nil {
		t.Fatal(err)
	}
	d.refreshPay(context.Background())
	d.refreshPay(context.Background())
	if calls != 1 || account != "0.0.4242" {
		t.Fatalf("activations %d for %q, want one for 0.0.4242", calls, account)
	}
}

func TestAnAccountWithAKeyIsLeftAlone(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"account":"0.0.4242","key":{"_type":"ECDSA_SECP256K1","key":"02ab"},"balance":{"balance":99000,"tokens":[]}}`)
	}))
	defer mirror.Close()
	d := payDaemon(t, mirror.URL)
	if err := d.SetPay(true, 0, 0); err != nil {
		t.Fatal(err)
	}
	d.refreshPay(context.Background())
	if d.Pay().Hollow {
		t.Fatal("an account with a key was called hollow")
	}
}

func TestAFundedAccountIsAssociatedWithUSDCOnce(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tokens") {
			_, _ = io.WriteString(w, `{"tokens":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"account":"0.0.4242","key":{"key":"02ab"},"balance":{"balance":99000,"tokens":[]}}`)
	}))
	defer mirror.Close()
	d := payDaemon(t, mirror.URL)
	var calls int
	d.associateFn = func(_ context.Context, a, key string) error {
		calls++
		if a != "0.0.4242" || key != d.hedera.PrivateKeyHex {
			t.Error("associated the wrong account or key")
		}
		return nil
	}
	if err := d.SetPay(true, 0, 0); err != nil {
		t.Fatal(err)
	}
	d.refreshPay(context.Background())
	d.refreshPay(context.Background())
	if calls != 1 {
		t.Fatalf("associations %d, want one", calls)
	}
}

func TestAuditOnlyDevicesAreNotAssociatedWithUSDC(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tokens") {
			_, _ = io.WriteString(w, `{"tokens":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"account":"0.0.4242","key":{"key":"02ab"},"balance":{"balance":99000,"tokens":[]}}`)
	}))
	defer mirror.Close()
	d := payDaemon(t, mirror.URL)
	d.associateFn = func(context.Context, string, string) error {
		t.Error("associated a device that does not pay")
		return nil
	}
	if err := d.SetAudit(true); err != nil {
		t.Fatal(err)
	}
	d.refreshPay(context.Background())
	if d.Pay().Account != "0.0.4242" {
		t.Fatal("the refresh did not run for an audit-only device")
	}
}
