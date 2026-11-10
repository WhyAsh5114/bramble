package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/WhyAsh5114/bramble/brambled"
	"github.com/WhyAsh5114/bramble/brambled/hederapay"
)

// Paying relays is opt-in (ADR 0021). The device makes its own Hedera testnet
// key the first time the user turns it on. That key pays relays that charge
// for data sessions, in testnet USDC, and cannot write tailnet records.

const (
	hederaKeyFile = "hedera.json"
	// DefaultMirror is the public Hedera testnet mirror node.
	DefaultMirror = "https://testnet.mirrornode.hedera.com"
	payRefresh    = 15 * time.Second
)

// PaySettings is saved in state.json.
type PaySettings struct {
	Enabled bool `json:"enabled"`
	// MaxSession and MaxDay cap what the node pays, in USDC's smallest unit
	// (six decimals). Zero means the node's default.
	MaxSession int64 `json:"maxSession,omitempty"`
	MaxDay     int64 `json:"maxDay,omitempty"`
}

// HederaKey is the device's own Hedera account key. It never leaves the device.
type HederaKey struct {
	PrivateKeyHex string    `json:"privateKey"`
	EVMAddress    string    `json:"evmAddress"`
	CreatedAt     time.Time `json:"createdAt"`
}

// LoadHederaKey reads the key, if one was made.
func (s *Store) LoadHederaKey() (HederaKey, bool, error) {
	var k HederaKey
	ok, err := s.readJSON(hederaKeyFile, &k)
	return k, ok && k.PrivateKeyHex != "", err
}

// LoadOrCreateHederaKey returns the key, making it on first use.
func (s *Store) LoadOrCreateHederaKey() (HederaKey, error) {
	k, ok, err := s.LoadHederaKey()
	if err != nil || ok {
		return k, err
	}
	priv, evm, err := hederapay.GenerateKey()
	if err != nil {
		return k, err
	}
	k = HederaKey{PrivateKeyHex: priv, EVMAddress: evm, CreatedAt: time.Now().UTC()}
	return k, s.writeJSON(hederaKeyFile, k)
}

// KeyPath is where the Hedera key is kept, for the UI to name.
func (s *Store) KeyPath() string { return s.path(hederaKeyFile) }

type payBalances struct {
	Account string
	HBAR    int64 // tinybar
	USDC    int64 // smallest unit
	// Hollow is true while the account has no key: it can receive but not send.
	Hollow bool
}

// PayInfo is what the UI shows about paying relays.
type PayInfo struct {
	Enabled    bool   `json:"enabled"`
	Network    string `json:"network"`
	Asset      string `json:"asset"`
	EVMAddress string `json:"evmAddress,omitempty"`
	// Account is the Hedera account ID. It is empty until the address has
	// been funded, because sending test HBAR or USDC to the address creates it.
	Account string `json:"account,omitempty"`
	// Hollow is true until the account's first signed transaction; the daemon
	// sends one once the account holds HBAR.
	Hollow bool  `json:"hollow"`
	HBAR   int64 `json:"hbarTinybar"`
	USDC   int64 `json:"usdc"`
	// Active is true when the running node pays relays that charge.
	Active     bool               `json:"active"`
	MaxSession int64              `json:"maxSession"`
	MaxDay     int64              `json:"maxDay"`
	SpentToday int64              `json:"spentToday"`
	Recent     []brambled.Payment `json:"recent"`
	KeyFile    string             `json:"keyFile,omitempty"`
	Error      string             `json:"error,omitempty"`
}

// Defaults match the node's (brambled.defaultMaxSession and defaultMaxDay).
const (
	defaultPayMaxSession = 500_000
	defaultPayMaxDay     = 2_000_000
)

func (d *Daemon) mirrorURL() string {
	if d.Mirror != "" {
		return d.Mirror
	}
	return DefaultMirror
}

// fetchBalances asks the mirror node about the account at evm. A 404 means the
// address has not been funded yet, which is not an error.
func fetchBalances(ctx context.Context, base, evm string) (payBalances, error) {
	var out payBalances
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/accounts/"+evm, nil)
	if err != nil {
		return out, err
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return out, errors.New("the Hedera mirror node did not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return out, nil
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("the Hedera mirror node answered %d", resp.StatusCode)
	}
	var body struct {
		Account string          `json:"account"`
		Key     json.RawMessage `json:"key"`
		Balance struct {
			Balance int64 `json:"balance"`
			Tokens  []struct {
				TokenID string `json:"token_id"`
				Balance int64  `json:"balance"`
			} `json:"tokens"`
		} `json:"balance"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&body); err != nil {
		return out, errors.New("the Hedera mirror node sent a reply this device cannot read")
	}
	out.Account, out.HBAR = body.Account, body.Balance.Balance
	out.Hollow = len(body.Key) == 0 || string(body.Key) == "null"
	for _, t := range body.Balance.Tokens {
		if t.TokenID == brambled.DefaultPayAsset {
			out.USDC = t.Balance
		}
	}
	return out, nil
}

// refreshPay updates the balances and, when the account first appears, wakes
// the reconciler so the node restarts with payments on.
func (d *Daemon) refreshPay(ctx context.Context) {
	d.mu.Lock()
	enabled, evm := d.hederaOnLocked(), ""
	if d.hedera != nil {
		evm = d.hedera.EVMAddress
	}
	prev := d.payBal.Account
	d.mu.Unlock()
	if !enabled || evm == "" {
		return
	}
	bal, err := fetchBalances(ctx, d.mirrorURL(), evm)
	d.mu.Lock()
	if err != nil {
		d.payErr = err.Error()
	} else {
		d.payErr, d.payBal, d.payAt = "", bal, time.Now()
	}
	d.mu.Unlock()
	if err == nil && bal.Account != "" && bal.Hollow && bal.HBAR > 0 {
		d.activate(ctx, bal.Account)
	}
	if err == nil && bal.Account != prev {
		d.poke()
	}
}

// activateRetry spaces out attempts to finish a hollow account.
const activateRetry = 2 * time.Minute

// activate sends the account's first signed transaction, so it can pay.
func (d *Daemon) activate(ctx context.Context, account string) {
	d.mu.Lock()
	if d.hedera == nil || time.Since(d.activateAt) < activateRetry {
		d.mu.Unlock()
		return
	}
	d.activateAt = time.Now()
	key := d.hedera.PrivateKeyHex
	d.mu.Unlock()
	fn := d.activateFn
	if fn == nil {
		fn = hederapay.Activate
	}
	err := fn(ctx, account, key)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.payErr = "Could not send the account's first transaction: " + err.Error()
		return
	}
	d.payBal.Hollow = false
}

func (d *Daemon) payLoop(ctx context.Context) {
	t := time.NewTicker(payRefresh)
	defer t.Stop()
	for {
		d.refreshPay(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// hederaOnLocked is true when either opt-in needs the device's Hedera key.
func (d *Daemon) hederaOnLocked() bool {
	return (d.saved.Pay != nil && d.saved.Pay.Enabled) || (d.saved.Audit != nil && d.saved.Audit.Enabled)
}

// payArgsLocked adds the node's Hedera flags. The key stays in-process.
func (d *Daemon) payArgsLocked() []string {
	if !d.hederaOnLocked() || d.hedera == nil || d.payBal.Account == "" {
		return nil
	}
	args := []string{"-hedera-account", d.payBal.Account, "-hedera-key", d.hedera.PrivateKeyHex}
	if p := d.saved.Pay; p == nil || !p.Enabled {
		args = append(args, "-hedera-no-pay")
	} else {
		if p.MaxSession > 0 {
			args = append(args, "-pay-max-session", strconv.FormatInt(p.MaxSession, 10))
		}
		if p.MaxDay > 0 {
			args = append(args, "-pay-max-day", strconv.FormatInt(p.MaxDay, 10))
		}
	}
	if a := d.saved.Audit; a != nil && a.Enabled && d.tn != nil {
		if topic := d.tn.AuditTopic(); topic != "" {
			args = append(args, "-audit-topic", topic)
		}
	}
	return args
}

// Pay returns the paying state for the UI.
func (d *Daemon) Pay() PayInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	info := PayInfo{Network: hederapay.NetworkTestnet, Asset: brambled.DefaultPayAsset, MaxSession: defaultPayMaxSession, MaxDay: defaultPayMaxDay, Recent: []brambled.Payment{}}
	// The key and balances show whenever the device has a Hedera key, because
	// the audit log uses it too.
	if d.hedera != nil {
		info.EVMAddress, info.KeyFile = d.hedera.EVMAddress, d.Store.KeyPath()
		info.Account, info.HBAR, info.USDC, info.Error = d.payBal.Account, d.payBal.HBAR, d.payBal.USDC, d.payErr
		info.Hollow = d.payBal.Account != "" && d.payBal.Hollow
	}
	p := d.saved.Pay
	if p == nil {
		return info
	}
	info.Enabled = p.Enabled
	if p.MaxSession > 0 {
		info.MaxSession = p.MaxSession
	}
	if p.MaxDay > 0 {
		info.MaxDay = p.MaxDay
	}
	if d.node != nil && d.node.handle != nil {
		if st, ok := d.node.handle.Payments(); ok {
			info.Active, info.SpentToday = true, st.SpentToday
			info.Recent = st.Recent
		}
	}
	return info
}

// SetPay turns paying on or off and sets the caps. The first time it is
// turned on it makes the device's Hedera key.
func (d *Daemon) SetPay(enabled bool, maxSession, maxDay int64) error {
	if maxSession < 0 || maxDay < 0 {
		return errors.New("a spend cap cannot be negative")
	}
	if maxSession > 0 && maxDay > 0 && maxSession > maxDay {
		return errors.New("the per-session cap cannot exceed the daily cap")
	}
	var key *HederaKey
	if enabled {
		k, err := d.Store.LoadOrCreateHederaKey()
		if err != nil {
			return err
		}
		key = &k
	}
	d.mu.Lock()
	d.saved.Pay = &PaySettings{Enabled: enabled, MaxSession: maxSession, MaxDay: maxDay}
	if key != nil {
		d.hedera = key
	}
	err := d.Store.SaveSaved(d.saved)
	d.mu.Unlock()
	d.poke()
	if enabled {
		go d.refreshPay(context.Background())
	}
	return err
}
