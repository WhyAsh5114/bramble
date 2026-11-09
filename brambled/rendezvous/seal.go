package rendezvous

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// Candidates are sealed end to end between the two peers (ADR 0019). Both
// derive the same key from X25519 of their WireGuard static keys; nobody else,
// the relay included, can compute it. Each side registers at the relay under a
// pseudonymous ID derived from that key, so the relay never sees a WireGuard
// public key and cannot tell which devices are talking. A sealed candidate
// carries its creation time; an old one is rejected, which limits replay.

const maxCandidateAge = 2 * time.Minute

var (
	identityMu sync.RWMutex
	identities = map[string][32]byte{} // public key hex -> private key
)

// RegisterIdentity makes a node's private key available to Exchange. It
// returns the public key hex. Exchange refuses to run without a registered
// identity, so a candidate is never sent in the clear.
func RegisterIdentity(privateHex string) (string, error) {
	raw, err := hex.DecodeString(privateHex)
	if err != nil || len(raw) != 32 {
		return "", errors.New("private key must be 32 bytes of hex")
	}
	var priv [32]byte
	copy(priv[:], raw)
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	pubHex := hex.EncodeToString(pub)
	identityMu.Lock()
	identities[pubHex] = priv
	identityMu.Unlock()
	return pubHex, nil
}

// pairKeys holds what two peers share for one exchange.
type pairKeys struct {
	aead   cipherAEAD
	myID   string
	peerID string
}

type cipherAEAD interface {
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
	NonceSize() int
}

func derivePair(ownPubHex, peerPubHex string) (*pairKeys, error) {
	identityMu.RLock()
	priv, ok := identities[ownPubHex]
	identityMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no identity registered for %s; call RegisterIdentity first", short(ownPubHex))
	}
	own, err := hex.DecodeString(ownPubHex)
	if err != nil {
		return nil, err
	}
	peer, err := hex.DecodeString(peerPubHex)
	if err != nil || len(peer) != 32 {
		return nil, fmt.Errorf("peer public key must be 32 bytes of hex")
	}
	shared, err := curve25519.X25519(priv[:], peer)
	if err != nil {
		return nil, fmt.Errorf("key agreement: %w", err)
	}
	lo, hi := own, peer
	if bytes.Compare(lo, hi) > 0 {
		lo, hi = hi, lo
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, []byte("bramble-rendezvous-v1"), append(append([]byte(nil), lo...), hi...)), key); err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	id := func(pub []byte) string {
		m := hmac.New(sha256.New, key)
		m.Write([]byte("id"))
		m.Write(pub)
		return hex.EncodeToString(m.Sum(nil)[:16])
	}
	return &pairKeys{aead: aead, myID: id(own), peerID: id(peer)}, nil
}

type sealedBody struct {
	Candidate string `json:"c"`
	Time      int64  `json:"t"`
}

func (k *pairKeys) seal(candidate string, now time.Time) (string, error) {
	plain, err := json.Marshal(sealedBody{Candidate: candidate, Time: now.Unix()})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := k.aead.Seal(nonce, nonce, plain, []byte("bramble-rdv|"+k.myID+"|"+k.peerID))
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func (k *pairKeys) open(sealed string, now time.Time) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(raw) < k.aead.NonceSize() {
		return "", errors.New("sealed candidate is malformed")
	}
	n := k.aead.NonceSize()
	plain, err := k.aead.Open(nil, raw[:n], raw[n:], []byte("bramble-rdv|"+k.peerID+"|"+k.myID))
	if err != nil {
		return "", errors.New("sealed candidate failed authentication")
	}
	var body sealedBody
	if err := json.Unmarshal(plain, &body); err != nil {
		return "", errors.New("sealed candidate is malformed")
	}
	age := now.Sub(time.Unix(body.Time, 0))
	if age > maxCandidateAge || age < -maxCandidateAge {
		return "", errors.New("sealed candidate is stale")
	}
	return body.Candidate, nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
