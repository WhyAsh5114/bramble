package hederapay

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"sort"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// maxNodes is how many nodes a payment is frozen for. A live test on
// 10 Nov 2026 showed Blocky402 accepts one or three nodes but answers
// "transaction_could_not_be_decoded" to the 4.9 KB transaction frozen for all
// seven testnet nodes. The reference client (@x402/hedera 2.25.0) freezes for
// a third of the nodes, which is three here.
const maxNodes = 3

// pickNodes returns up to n node account IDs from the client's network, in a
// fixed order so the same network gives the same transaction shape.
func pickNodes(c *hiero.Client, n int) []hiero.AccountID {
	seen := map[hiero.AccountID]bool{}
	var out []hiero.AccountID
	for _, id := range c.GetNetwork() {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Signer holds the node's own Hedera account and key. The key is separate from
// the tailnet wallet and cannot write tailnet records. It is never printed or
// logged.
type Signer struct {
	account hiero.AccountID
	key     hiero.PrivateKey
	// AllowHBAR lets the signer pay in HBAR (asset "0.0.0"). It is off by
	// default: a node pays in a token. Only a live test turns it on, to prove
	// the facilitator accepts the transaction when no token is at hand.
	AllowHBAR bool
}

// NewSigner parses an account ID like "0.0.1234" and an ECDSA secp256k1
// private key in hex.
func NewSigner(accountID, privateKeyHex string) (*Signer, error) {
	account, err := hiero.AccountIDFromString(accountID)
	if err != nil {
		return nil, fmt.Errorf("hedera account ID: %w", err)
	}
	key, err := hiero.PrivateKeyFromStringECDSA(privateKeyHex)
	if err != nil {
		// Never include the input: it is a secret.
		return nil, errors.New("hedera private key is not a valid ECDSA key")
	}
	return &Signer{account: account, key: key}, nil
}

// Account returns the account ID.
func (s *Signer) Account() string { return s.account.String() }

// PartialTransfer builds the transfer a requirement asks for, with the fee
// payer named in req.Extra["feePayer"] as the transaction's payer, signs it
// with the node's key, and returns it base64-encoded. The fee payer's
// signature is still missing; the facilitator adds it.
//
// It follows the reference client (@x402/hedera 2.25.0): an HTS token
// transfer from this account to payTo, a transaction ID generated for the fee
// payer, frozen against a testnet network, then signed once.
func (s *Signer) PartialTransfer(req Requirements) (string, error) {
	if req.Scheme != schemeExact {
		return "", fmt.Errorf("unsupported scheme %q", req.Scheme)
	}
	if req.Network != NetworkTestnet {
		return "", fmt.Errorf("unsupported network %q: payments run on %s only", req.Network, NetworkTestnet)
	}
	hbar := req.Asset == "0.0.0"
	if req.Asset == "" || (hbar && !s.AllowHBAR) {
		return "", errors.New("the node pays in a token, not HBAR")
	}
	amount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok || amount.Sign() <= 0 || !amount.IsInt64() {
		return "", fmt.Errorf("invalid amount %q", req.Amount)
	}
	feePayer, err := hiero.AccountIDFromString(req.Extra["feePayer"])
	if err != nil {
		return "", errors.New("the 402 response names no valid fee payer")
	}
	payTo, err := hiero.AccountIDFromString(req.PayTo)
	if err != nil {
		return "", fmt.Errorf("invalid payTo: %w", err)
	}
	var token hiero.TokenID
	if !hbar {
		if token, err = hiero.TokenIDFromString(req.Asset); err != nil {
			return "", fmt.Errorf("invalid asset: %w", err)
		}
	}
	if payTo == s.account {
		return "", errors.New("refusing to pay this node's own account")
	}
	if feePayer == s.account {
		return "", errors.New("refusing to use this node's own account as fee payer")
	}

	tx := hiero.NewTransferTransaction()
	if hbar {
		tx.AddHbarTransfer(s.account, hiero.HbarFromTinybar(-amount.Int64())).AddHbarTransfer(payTo, hiero.HbarFromTinybar(amount.Int64()))
	} else {
		tx.AddTokenTransfer(token, s.account, -amount.Int64()).AddTokenTransfer(token, payTo, amount.Int64())
	}
	tx.SetTransactionID(hiero.TransactionIDGenerate(feePayer))
	// ClientForTestnet does not connect. Freezing only reads its node list.
	client := hiero.ClientForTestnet()
	tx.SetNodeAccountIDs(pickNodes(client, maxNodes))
	frozen, err := tx.FreezeWith(client)
	if err != nil {
		return "", fmt.Errorf("freezing the transfer: %w", err)
	}
	raw, err := frozen.Sign(s.key).ToBytes()
	if err != nil {
		return "", fmt.Errorf("encoding the transfer: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
