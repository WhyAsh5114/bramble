package hederapay

import (
	"context"
	"errors"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// GenerateKey makes a new ECDSA key for a node's own Hedera account. It
// returns the private key as hex, which the caller must keep secret, and the
// EVM address (0x...) that identifies the account before it exists: sending
// test HBAR or a token to that address creates the account (auto-creation).
func GenerateKey() (privateKeyHex, evmAddress string, err error) {
	key, err := hiero.PrivateKeyGenerateEcdsa()
	if err != nil {
		return "", "", err
	}
	return key.StringRaw(), "0x" + key.PublicKey().ToEvmAddress(), nil
}

// EVMAddress derives the 0x address of a private key given as hex.
func EVMAddress(privateKeyHex string) (string, error) {
	key, err := hiero.PrivateKeyFromStringECDSA(privateKeyHex)
	if err != nil {
		// Never include the input: it is a secret.
		return "", errors.New("hedera private key is not a valid ECDSA key")
	}
	return "0x" + key.PublicKey().ToEvmAddress(), nil
}

// Activate finishes an account that auto-creation left hollow. Sending HBAR to
// an EVM address makes an account with no key; it can receive but not send
// until its own key signs a first transaction. This one updates the account's
// memo, which costs a fraction of a cent of test HBAR and needs no recipient.
func Activate(ctx context.Context, accountID, privateKeyHex string) error {
	acct, err := hiero.AccountIDFromString(accountID)
	if err != nil {
		return errors.New("hedera account ID is not valid")
	}
	key, err := hiero.PrivateKeyFromStringECDSA(privateKeyHex)
	if err != nil {
		return errors.New("hedera private key is not a valid ECDSA key")
	}
	c := client(acct, key)
	defer c.Close()
	tx, err := hiero.NewAccountUpdateTransaction().SetAccountID(acct).SetAccountMemo("bramble").FreezeWith(c)
	if err != nil {
		return err
	}
	resp, err := tx.Sign(key).Execute(c)
	if err != nil {
		return err
	}
	rc, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := resp.GetReceipt(c); done <- err }()
	select {
	case err := <-done:
		return err
	case <-rc.Done():
		return rc.Err()
	}
}

// Associate links the account to a token, so it can hold it. An account with
// automatic associations still needs this for faucets that check it first.
func Associate(ctx context.Context, accountID, privateKeyHex, tokenID string) error {
	acct, err := hiero.AccountIDFromString(accountID)
	if err != nil {
		return errors.New("hedera account ID is not valid")
	}
	token, err := hiero.TokenIDFromString(tokenID)
	if err != nil {
		return errors.New("hedera token ID is not valid")
	}
	key, err := hiero.PrivateKeyFromStringECDSA(privateKeyHex)
	if err != nil {
		return errors.New("hedera private key is not a valid ECDSA key")
	}
	c := client(acct, key)
	defer c.Close()
	tx, err := hiero.NewTokenAssociateTransaction().SetAccountID(acct).AddTokenID(token).FreezeWith(c)
	if err != nil {
		return err
	}
	resp, err := tx.Sign(key).Execute(c)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { _, err := resp.GetReceipt(c); done <- err }()
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		return errors.New("timed out waiting for the Hedera receipt")
	}
}
