package hederapay

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/sdk"
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"google.golang.org/protobuf/proto"
)

const (
	testPayer    = "0.0.1001"
	testPayTo    = "0.0.9000"
	testFeePayer = "0.0.7000"
	testAsset    = "0.0.429274"
)

// testSigner uses a throwaway key. No real key is read in a test.
func testSigner(t *testing.T) (*Signer, hiero.PrivateKey) {
	t.Helper()
	key, err := hiero.PrivateKeyGenerateEcdsa()
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSigner(testPayer, key.StringRaw())
	if err != nil {
		t.Fatal(err)
	}
	return s, key
}

func testRequirements() Requirements {
	return Requirements{
		Scheme: "exact", Network: NetworkTestnet, Amount: "500", Asset: testAsset, PayTo: testPayTo,
		MaxTimeoutSeconds: 180, Extra: map[string]string{"feePayer": testFeePayer},
	}
}

func decode(t *testing.T, b64 string) *hiero.TransferTransaction {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := hiero.TransactionFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	tt, ok := tx.(hiero.TransferTransaction)
	if !ok {
		p, ok := tx.(*hiero.TransferTransaction)
		if !ok {
			t.Fatalf("not a TransferTransaction: %T", tx)
		}
		return p
	}
	return &tt
}

// TestPartialTransferLayout checks the transaction against the verification
// rules of the x402 Hedera exact scheme. It proves the layout. It does not
// prove a facilitator accepts it; that needs a live run.
func TestPartialTransferLayout(t *testing.T) {
	s, key := testSigner(t)
	b64, err := s.PartialTransfer(testRequirements())
	if err != nil {
		t.Fatal(err)
	}
	tx := decode(t, b64)

	id := tx.GetTransactionID()
	if id.AccountID == nil || id.AccountID.String() != testFeePayer {
		t.Fatalf("the transaction ID must belong to the fee payer, got %+v", id.AccountID)
	}

	if n := len(tx.GetHbarTransfers()); n != 0 {
		t.Fatalf("expected no HBAR transfers, got %d", n)
	}
	transfers := tx.GetTokenTransfers()
	if len(transfers) != 1 {
		t.Fatalf("expected one asset, got %d", len(transfers))
	}
	var net int64
	got := map[string]int64{}
	for token, list := range transfers {
		if token.String() != testAsset {
			t.Fatalf("paid in %s, expected %s", token, testAsset)
		}
		for _, e := range list {
			got[e.AccountID.String()] += e.Amount
			net += e.Amount
		}
	}
	if net != 0 {
		t.Fatalf("net token transfer is %d, must be zero", net)
	}
	if got[testPayer] != -500 || got[testPayTo] != 500 || len(got) != 2 {
		t.Fatalf("unexpected transfers %v", got)
	}
	if got[testFeePayer] < 0 {
		t.Fatal("the fee payer must never be debited")
	}

	if !key.PublicKey().VerifyTransaction(tx) {
		t.Fatal("the payer's signature does not verify")
	}
	other, err := hiero.PrivateKeyGenerateEcdsa()
	if err != nil {
		t.Fatal(err)
	}
	if other.PublicKey().VerifyTransaction(tx) {
		t.Fatal("a key that did not sign verifies")
	}
	// The fee payer signs later. Until then every node's copy of the
	// transaction carries exactly one signature pair.
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	var list sdk.TransactionList
	if err := proto.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.TransactionList) == 0 {
		t.Fatal("no transactions in the payload")
	}
	for i, wire := range list.TransactionList {
		var signed services.SignedTransaction
		if err := proto.Unmarshal(wire.SignedTransactionBytes, &signed); err != nil {
			t.Fatal(err)
		}
		if n := len(signed.SigMap.GetSigPair()); n != 1 {
			t.Fatalf("copy %d has %d signature pairs, expected the payer's alone", i, n)
		}
	}
}

func TestPartialTransferRefusesWhatItShouldNotSign(t *testing.T) {
	s, _ := testSigner(t)
	cases := map[string]func(*Requirements){
		"mainnet":          func(r *Requirements) { r.Network = "hedera:mainnet" },
		"another scheme":   func(r *Requirements) { r.Scheme = "upto" },
		"hbar":             func(r *Requirements) { r.Asset = "0.0.0" },
		"no asset":         func(r *Requirements) { r.Asset = "" },
		"zero amount":      func(r *Requirements) { r.Amount = "0" },
		"negative amount":  func(r *Requirements) { r.Amount = "-5" },
		"text amount":      func(r *Requirements) { r.Amount = "five" },
		"huge amount":      func(r *Requirements) { r.Amount = "99999999999999999999999" },
		"no fee payer":     func(r *Requirements) { r.Extra = nil },
		"bad fee payer":    func(r *Requirements) { r.Extra = map[string]string{"feePayer": "nope"} },
		"bad payTo":        func(r *Requirements) { r.PayTo = "nope" },
		"pay self":         func(r *Requirements) { r.PayTo = testPayer },
		"self as feePayer": func(r *Requirements) { r.Extra = map[string]string{"feePayer": testPayer} },
		"bad asset":        func(r *Requirements) { r.Asset = "usdc" },
	}
	for name, mutate := range cases {
		r := testRequirements()
		mutate(&r)
		if _, err := s.PartialTransfer(r); err == nil {
			t.Errorf("%s: signed a transfer it should refuse", name)
		}
	}
}

func TestBadKeyErrorNeverEchoesTheKey(t *testing.T) {
	const secret = "not-a-key-but-pretend-it-is-secret-1234"
	_, err := NewSigner(testPayer, secret)
	if err == nil {
		t.Fatal("accepted a bad key")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("the error repeats the key")
	}
	if _, err := NewSigner("nope", secret); err == nil {
		t.Fatal("accepted a bad account")
	}
}
