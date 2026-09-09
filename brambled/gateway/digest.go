// Package gateway is Phase 2 Section C's enforcement side: a fixed-port
// CONNECT listener that checks a requesting peer's on-chain acl digests
// against this gateway's own trusted acl-granters before proxying to a local
// service. See docs/adr/0005-acl-record-schema.md for the record schema
// Section B built (read-path only) and docs/adr/0006-gateway-connect-protocol.md
// for this package's own design.
package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// aclHKDFInfo domain-separates this derivation from WireGuard's own Noise_IK
// use of the same raw ECDH output over the same keypair — see adr/0005.
const aclHKDFInfo = "bramble-acl-v1"

// ACLDigestECDH reproduces admincli/src/roles.ts's aclDigestECDH byte-for-byte:
//
//	sharedSecret = X25519(myPrivateKey, theirPublicKey)
//	aclKey       = HKDF-SHA256(ikm=sharedSecret, salt=none, info="bramble-acl-v1", length=32)
//	digest       = hex(HMAC-SHA256(key=aclKey, message=utf8(trim(lowercase(serviceName)))))
//
// A granter and a gateway each compute this independently from their own
// private key plus the other's already-published pubkey — by Diffie-Hellman
// symmetry the two calls (with arguments swapped) produce the identical
// digest, with nothing ever transmitted between them (adr/0005). Both
// arguments are hex-encoded, unprefixed 32-byte X25519 keys, matching the raw
// WireGuard key format brambled/wgnode already uses.
func ACLDigestECDH(myPrivateKeyHex, theirPublicKeyHex, serviceName string) (string, error) {
	priv, err := decodeKey32(myPrivateKeyHex)
	if err != nil {
		return "", fmt.Errorf("decoding private key: %w", err)
	}
	pub, err := decodeKey32(theirPublicKeyHex)
	if err != nil {
		return "", fmt.Errorf("decoding public key: %w", err)
	}

	shared, err := curve25519.X25519(priv, pub)
	if err != nil {
		return "", fmt.Errorf("computing X25519 shared secret: %w", err)
	}

	aclKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, nil, []byte(aclHKDFInfo)), aclKey); err != nil {
		return "", fmt.Errorf("deriving ACL key: %w", err)
	}

	canonical := strings.ToLower(strings.TrimSpace(serviceName))
	mac := hmac.New(sha256.New, aclKey)
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func decodeKey32(hexKey string) ([]byte, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(raw))
	}
	return raw, nil
}
