package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Rendezvous payment token verification (docs/adr/0007). Tokens are minted
// by relay-sidecar (relay-sidecar/src/token.ts) after a real x402/Hedera
// payment settles, and verified here entirely locally — no callback to the
// sidecar on the per-connection hot path. The wire format must match
// relay-sidecar/src/token.ts byte-for-byte:
//
//	payload = {"exp":<unix seconds>,"nonce":"<32 hex chars>"}   (fixed key order)
//	token   = base64url(payload) + "." + hex(HMAC-SHA256(secret, base64url(payload)))

type tokenPayload struct {
	Exp   int64  `json:"exp"`
	Nonce string `json:"nonce"`
}

// tokenVerifier checks rendezvous tokens against a shared secret and
// remembers consumed nonces to reject replay, for the life of one relay
// process. Safe for concurrent use.
type tokenVerifier struct {
	secret []byte

	mu   sync.Mutex
	seen map[string]int64 // nonce -> expiry (unix seconds), for lazy pruning
}

func newTokenVerifier(secret []byte) *tokenVerifier {
	return &tokenVerifier{secret: secret, seen: make(map[string]int64)}
}

// verify checks signature, expiry, and single-use, and marks the token's
// nonce consumed on success. Every failure mode returns a distinct message
// so a demo-time failure is legible on camera rather than a bare "invalid".
func (v *tokenVerifier) verify(token string, now time.Time) error {
	payloadB64, sig, ok := strings.Cut(token, ".")
	if !ok {
		return errors.New("malformed token")
	}

	expected := hmac.New(sha256.New, v.secret)
	expected.Write([]byte(payloadB64))
	expectedSig := hex.EncodeToString(expected.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return errors.New("invalid token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		// A token that passed the HMAC check but doesn't even decode as
		// base64url can't have been minted by us — treat identically to a
		// bad signature rather than a separate error class.
		return errors.New("invalid token signature")
	}
	var payload tokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return fmt.Errorf("malformed token payload: %w", err)
	}

	nowUnix := now.Unix()
	if payload.Exp <= nowUnix {
		return errors.New("expired token")
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.prune(nowUnix)
	if _, dup := v.seen[payload.Nonce]; dup {
		return errors.New("token already used")
	}
	v.seen[payload.Nonce] = payload.Exp
	return nil
}

// prune drops nonces whose token has already expired — once a token is
// expired, verify's own expiry check rejects it regardless of whether the
// nonce is still remembered, so there's no reuse window from pruning early.
// Caller holds v.mu.
func (v *tokenVerifier) prune(nowUnix int64) {
	for nonce, exp := range v.seen {
		if exp <= nowUnix {
			delete(v.seen, nonce)
		}
	}
}
