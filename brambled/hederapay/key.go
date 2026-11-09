package hederapay

import (
	"errors"

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
