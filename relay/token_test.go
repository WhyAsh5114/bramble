package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func mustSecret(t *testing.T) []byte {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("generating secret: %v", err)
	}
	return secret
}

// mintForTest reimplements relay-sidecar/src/token.ts's mintToken locally,
// so this test exercises the real wire format without needing the TS
// sidecar running — see docs/adr/0007 for the format both sides must match.
func mintForTest(secret []byte, exp int64, nonce string) string {
	payload := fmt.Sprintf(`{"exp":%d,"nonce":"%s"}`, exp, nonce)
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payloadB64))
	return payloadB64 + "." + hex.EncodeToString(mac.Sum(nil))
}

func TestTokenVerifier_ValidTokenAccepted(t *testing.T) {
	secret := mustSecret(t)
	v := newTokenVerifier(secret)
	now := time.Unix(1_800_000_000, 0)

	token := mintForTest(secret, now.Add(60*time.Second).Unix(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := v.verify(token, now); err != nil {
		t.Fatalf("expected valid token to be accepted, got: %v", err)
	}
}

func TestTokenVerifier_ExpiredTokenRejected(t *testing.T) {
	secret := mustSecret(t)
	v := newTokenVerifier(secret)
	now := time.Unix(1_800_000_000, 0)

	token := mintForTest(secret, now.Add(-1*time.Second).Unix(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err := v.verify(token, now); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestTokenVerifier_ReplayedTokenRejected(t *testing.T) {
	secret := mustSecret(t)
	v := newTokenVerifier(secret)
	now := time.Unix(1_800_000_000, 0)

	token := mintForTest(secret, now.Add(60*time.Second).Unix(), "cccccccccccccccccccccccccccccccc")
	if err := v.verify(token, now); err != nil {
		t.Fatalf("first use should succeed, got: %v", err)
	}
	if err := v.verify(token, now); err == nil {
		t.Fatal("expected second use of the same token to be rejected")
	}
}

func TestTokenVerifier_WrongSignatureRejected(t *testing.T) {
	secret := mustSecret(t)
	otherSecret := mustSecret(t)
	v := newTokenVerifier(secret)
	now := time.Unix(1_800_000_000, 0)

	token := mintForTest(otherSecret, now.Add(60*time.Second).Unix(), "dddddddddddddddddddddddddddddddd")
	if err := v.verify(token, now); err == nil {
		t.Fatal("expected token signed with the wrong secret to be rejected")
	}
}

func TestTokenVerifier_MalformedTokenRejected(t *testing.T) {
	v := newTokenVerifier(mustSecret(t))
	now := time.Unix(1_800_000_000, 0)

	for _, bad := range []string{"", "no-dot-here", "onlyoneside.", ".onlysig"} {
		if err := v.verify(bad, now); err == nil {
			t.Fatalf("expected malformed token %q to be rejected", bad)
		}
	}
}
