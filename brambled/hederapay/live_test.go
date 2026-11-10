//go:build live

package hederapay

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// Live tests talk to the Blocky402 testnet facilitator and spend a few
// thousand tinybar of test HBAR. They run only with `-tags live` and need
// HEDERA_CLIENT_ACCOUNT_ID, HEDERA_CLIENT_PRIVATE_KEY and
// HEDERA_RELAY_ACCOUNT_ID (the pay-to account) in the environment. The key is
// passed straight into the signer and never printed.
//
//	cd brambled && go test -tags live -run TestLive -v ./hederapay/
const liveFacilitator = "https://api.testnet.blocky402.com"

func liveSigner(t *testing.T) (*Signer, string) {
	t.Helper()
	id, key, payTo := os.Getenv("HEDERA_CLIENT_ACCOUNT_ID"), os.Getenv("HEDERA_CLIENT_PRIVATE_KEY"), os.Getenv("HEDERA_RELAY_ACCOUNT_ID")
	if id == "" || key == "" || payTo == "" {
		t.Skip("HEDERA_CLIENT_ACCOUNT_ID, HEDERA_CLIENT_PRIVATE_KEY and HEDERA_RELAY_ACCOUNT_ID are not set")
	}
	s, err := NewSigner(id, key)
	if err != nil {
		t.Fatal(err)
	}
	s.AllowHBAR = true // a test with no USDC at hand; a real node pays in a token
	return s, payTo
}

func TestLiveFacilitatorVerifiesOurTransaction(t *testing.T) {
	s, payTo := liveSigner(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := &relaysrv.Facilitator{BaseURL: liveFacilitator}
	feePayer, err := f.FeePayer(ctx, NetworkTestnet)
	if err != nil {
		t.Fatal(err)
	}
	req := Requirements{Scheme: "exact", Network: NetworkTestnet, Amount: "1000", Asset: "0.0.0", PayTo: payTo, MaxTimeoutSeconds: 180, Extra: map[string]string{"feePayer": feePayer}}
	tx, err := s.PartialTransfer(req)
	if err != nil {
		t.Fatal(err)
	}
	payload := relaysrv.PaymentPayload{X402Version: 2, Resource: relaysrv.ResourceInfo{URL: "https://relay.test/x402/session"}, Accepted: relaysrv.PaymentRequirements(req)}
	payload.Payload.Transaction = tx
	v, err := f.Verify(ctx, payload, relaysrv.PaymentRequirements(req))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("verify: isValid=%v reason=%q payer=%s feePayer=%s", v.IsValid, v.InvalidReason, v.Payer, feePayer)
	if !v.IsValid {
		t.Fatalf("the facilitator rejected the transaction: %s", v.InvalidReason)
	}
}

func TestLivePaymentThroughARealRelayBuysASession(t *testing.T) {
	s, payTo := liveSigner(t)
	l, err := relaysrv.StartLocalPaid(relaysrv.Limits{
		SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 4, SessionsPerHour: 100, PortMin: 52200, PortMax: 52209,
	}, nil, relaysrv.PaywallConfig{
		Facilitator: &relaysrv.Facilitator{BaseURL: liveFacilitator}, Network: NetworkTestnet, Asset: "0.0.0",
		PayTo: payTo, PricePerMiB: 2000, // 1000 tinybar for half a MiB
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	relay := rendezvous.Relay{Label: "live", Address: l.Addr, Pin: l.Pin}
	buyer := &Buyer{Signer: s, MaxAmount: map[string]int64{"0.0.0": 5000}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	p, err := buyer.BuySession(ctx, rendezvous.HTTPClient(relay, 90*time.Second), "https://"+l.Addr, 524288)
	if err != nil {
		t.Fatalf("paying through the live facilitator: %v", err)
	}
	t.Logf("settled: %s  https://hashscan.io/testnet/transaction/%s", p.Transaction, p.Transaction)
	grant, err := rendezvous.RequestSessionWithToken(relay, 0, p.Token, 5*time.Second)
	if err != nil || grant.Bytes != 524288 {
		t.Fatalf("grant %+v err %v", grant, err)
	}

	// Confirm on the public mirror node that the money moved as stated.
	id := strings.Replace(strings.Replace(p.Transaction, "@", "-", 1), ".", "-", -1)
	id = strings.Replace(id, "0-0-", "0.0.", 1)
	var rec struct {
		Transactions []struct {
			Result    string `json:"result"`
			Name      string `json:"name"`
			Transfers []struct {
				Account string `json:"account"`
				Amount  int64  `json:"amount"`
			} `json:"transfers"`
		} `json:"transactions"`
	}
	var last error
	for i := 0; i < 12; i++ {
		resp, err := http.Get("https://testnet.mirrornode.hedera.com/api/v1/transactions/" + id)
		if err == nil && resp.StatusCode == http.StatusOK {
			last = json.NewDecoder(resp.Body).Decode(&rec)
			resp.Body.Close()
			if last == nil && len(rec.Transactions) > 0 {
				break
			}
		} else if err == nil {
			resp.Body.Close()
		}
		time.Sleep(3 * time.Second)
	}
	if len(rec.Transactions) == 0 {
		t.Fatalf("the mirror node has no record of %s (%v)", id, last)
	}
	tr := rec.Transactions[0]
	var paid, got int64
	for _, x := range tr.Transfers {
		if x.Account == s.Account() {
			paid = x.Amount
		}
		if x.Account == payTo {
			got = x.Amount
		}
	}
	t.Logf("mirror node: %s %s; payer %d, pay-to %d tinybar", tr.Name, tr.Result, paid, got)
	if tr.Result != "SUCCESS" || paid != -1000 || got != 1000 {
		t.Fatalf("settlement does not match the payment: result %s payer %d payTo %d", tr.Result, paid, got)
	}
}

// Creates an audit topic with the test account, posts one commitment as a
// gateway would, and finds it again through the public mirror node. Costs a
// fraction of a cent of test HBAR.
func TestLiveAuditCommitmentReachesTheTopic(t *testing.T) {
	s, _ := liveSigner(t)
	topic, err := CreateTopic(s, "bramble audit trail v1 (test)")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("topic %s", topic)
	pub, err := NewPublisher(s, topic)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	go pub.Run(ctx)
	pub.Record(Event{"gateway": "server", "requester": "laptop", "service": "web", "outcome": "allowed", "at": "1700000000"})
	o := pub.Openings()[0]
	var e *Entry
	for i := 0; i < 30 && e == nil; i++ {
		time.Sleep(3 * time.Second)
		if e, err = FindCommitment(ctx, http.DefaultClient, "https://testnet.mirrornode.hedera.com", topic, o.Commitment); err != nil {
			t.Fatal(err)
		}
	}
	if e == nil {
		t.Fatalf("the commitment never reached the topic: %+v", pub.Stats())
	}
	t.Logf("found at sequence %d, consensus %s", e.Sequence, e.Timestamp)
	forged := o
	forged.Event = Event{"gateway": "server", "requester": "laptop", "service": "web", "outcome": "denied", "at": "1700000000"}
	c, _ := forged.Recompute()
	if f, _ := FindCommitment(ctx, http.DefaultClient, "https://testnet.mirrornode.hedera.com", topic, c); f != nil {
		t.Fatal("a forged opening matched")
	}
}

// A fresh key is funded by sending HBAR to its EVM address, which makes a
// hollow account. Activate must give it a key, so it can sign.
func TestLiveActivateFinishesAHollowAccount(t *testing.T) {
	id, key := os.Getenv("HEDERA_CLIENT_ACCOUNT_ID"), os.Getenv("HEDERA_CLIENT_PRIVATE_KEY")
	if id == "" || key == "" {
		t.Skip("HEDERA_CLIENT_ACCOUNT_ID and HEDERA_CLIENT_PRIVATE_KEY are not set")
	}
	priv, evm, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	acct, err := hiero.AccountIDFromString(id)
	if err != nil {
		t.Fatal(err)
	}
	opKey, err := hiero.PrivateKeyFromStringECDSA(key)
	if err != nil {
		opKey, err = hiero.PrivateKeyFromString(key)
		if err != nil {
			t.Fatal("client key is not readable")
		}
	}
	c := client(acct, opKey)
	defer c.Close()
	to, err := hiero.AccountIDFromEvmAddress(0, 0, evm[2:])
	if err != nil {
		t.Fatal(err)
	}
	resp, err := hiero.NewTransferTransaction().AddHbarTransfer(acct, hiero.HbarFromTinybar(-50_000_000)).AddHbarTransfer(to, hiero.HbarFromTinybar(50_000_000)).Execute(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resp.GetReceipt(c); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	look := func() (account string, hollow bool) {
		for i := 0; i < 30; i++ {
			r, err := http.Get("https://testnet.mirrornode.hedera.com/api/v1/accounts/" + evm)
			if err == nil {
				var b struct {
					Account string          `json:"account"`
					Key     json.RawMessage `json:"key"`
				}
				_ = json.NewDecoder(r.Body).Decode(&b)
				r.Body.Close()
				if b.Account != "" {
					return b.Account, len(b.Key) == 0 || string(b.Key) == "null"
				}
			}
			time.Sleep(2 * time.Second)
		}
		return "", false
	}
	account, hollow := look()
	if account == "" || !hollow {
		t.Fatalf("expected a hollow account after funding, got %q hollow=%v", account, hollow)
	}
	if err := Activate(ctx, account, priv); err != nil {
		t.Fatal(err)
	}
	var now bool
	for i := 0; i < 15; i++ {
		time.Sleep(3 * time.Second)
		if _, now = look(); !now {
			break
		}
	}
	if now {
		t.Fatal("the account is still hollow after Activate")
	}
	t.Logf("account %s was hollow and now has a key", account)
}

// The Circle faucet only sends USDC to an account that is associated with it.
// This associates the client account, if it is not already.
func TestLiveAssociateUSDC(t *testing.T) {
	id, key := os.Getenv("HEDERA_CLIENT_ACCOUNT_ID"), os.Getenv("HEDERA_CLIENT_PRIVATE_KEY")
	if id == "" || key == "" {
		t.Skip("HEDERA_CLIENT_ACCOUNT_ID and HEDERA_CLIENT_PRIVATE_KEY are not set")
	}
	const usdc = "0.0.429274"
	has := func() bool {
		r, err := http.Get("https://testnet.mirrornode.hedera.com/api/v1/accounts/" + id + "/tokens?token.id=" + usdc)
		if err != nil {
			return false
		}
		defer r.Body.Close()
		var b struct {
			Tokens []struct{} `json:"tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		return len(b.Tokens) > 0
	}
	if !has() {
		if err := Associate(context.Background(), id, key, usdc); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 15 && !has(); i++ {
			time.Sleep(3 * time.Second)
		}
	}
	if !has() {
		t.Fatal("the account is not associated with USDC")
	}
}
