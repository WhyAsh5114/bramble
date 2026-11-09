package hederapay

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// Signer holds the node's own Hedera account and key. The key is separate from
// the tailnet wallet and cannot write tailnet records. It is never printed or
// logged.
type Signer struct {
	account hiero.AccountID
	key     hiero.PrivateKey
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
	if req.Asset == "" || req.Asset == "0.0.0" {
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
	token, err := hiero.TokenIDFromString(req.Asset)
	if err != nil {
		return "", fmt.Errorf("invalid asset: %w", err)
	}
	if payTo == s.account {
		return "", errors.New("refusing to pay this node's own account")
	}
	if feePayer == s.account {
		return "", errors.New("refusing to use this node's own account as fee payer")
	}

	tx := hiero.NewTransferTransaction().
		AddTokenTransfer(token, s.account, -amount.Int64()).
		AddTokenTransfer(token, payTo, amount.Int64()).
		SetTransactionID(hiero.TransactionIDGenerate(feePayer))
	// ClientForTestnet does not connect. Freezing only reads its node list.
	frozen, err := tx.FreezeWith(hiero.ClientForTestnet())
	if err != nil {
		return "", fmt.Errorf("freezing the transfer: %w", err)
	}
	raw, err := frozen.Sign(s.key).ToBytes()
	if err != nil {
		return "", fmt.Errorf("encoding the transfer: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
