package relaysrv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// x402 paid data sessions (ADR 0021).
//
// A relay in paid mode answers POST /x402/session over HTTP with 402 Payment
// Required. The node pays with the x402 "exact" scheme on Hedera, the
// facilitator verifies and settles, and the relay mints a single-use token.
// The node presents the token in the rendezvous "session" message.
//
// The relay builds every PaymentRequirements itself and sends only those to
// the facilitator. It never trusts the "accepted" copy a client returns.

const (
	x402Version        = 2
	headerRequired     = "PAYMENT-REQUIRED"
	headerSignature    = "PAYMENT-SIGNATURE"
	headerResponse     = "PAYMENT-RESPONSE"
	schemeExact        = "exact"
	maxPaymentHeader   = 48 << 10
	tokenTTL           = 10 * time.Minute
	facilitatorTimeout = 30 * time.Second
	supportedTTL       = 10 * time.Minute
	bytesPerMiB        = 1 << 20
)

// PaymentRequirements is one accepted way to pay (x402 core spec).
type PaymentRequirements struct {
	Scheme            string            `json:"scheme"`
	Network           string            `json:"network"`
	Amount            string            `json:"amount"`
	Asset             string            `json:"asset"`
	PayTo             string            `json:"payTo"`
	MaxTimeoutSeconds int               `json:"maxTimeoutSeconds"`
	Extra             map[string]string `json:"extra,omitempty"`
}

// ResourceInfo describes the paid resource.
type ResourceInfo struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// PaymentRequired is the body of the PAYMENT-REQUIRED header.
type PaymentRequired struct {
	X402Version int                   `json:"x402Version"`
	Error       string                `json:"error,omitempty"`
	Resource    ResourceInfo          `json:"resource"`
	Accepts     []PaymentRequirements `json:"accepts"`
}

// PaymentPayload is the body of the PAYMENT-SIGNATURE header.
type PaymentPayload struct {
	X402Version int                 `json:"x402Version"`
	Resource    ResourceInfo        `json:"resource"`
	Accepted    PaymentRequirements `json:"accepted"`
	Payload     struct {
		Transaction string `json:"transaction"`
	} `json:"payload"`
}

// SettlementResponse is the body of the PAYMENT-RESPONSE header.
type SettlementResponse struct {
	Success     bool   `json:"success"`
	ErrorReason string `json:"errorReason,omitempty"`
	Payer       string `json:"payer,omitempty"`
	Transaction string `json:"transaction"`
	Network     string `json:"network"`
}

type facilitatorRequest struct {
	X402Version         int                 `json:"x402Version"`
	PaymentPayload      PaymentPayload      `json:"paymentPayload"`
	PaymentRequirements PaymentRequirements `json:"paymentRequirements"`
}

type verifyResponse struct {
	IsValid       bool   `json:"isValid"`
	InvalidReason string `json:"invalidReason,omitempty"`
	Payer         string `json:"payer,omitempty"`
}

type supportedResponse struct {
	Kinds []struct {
		X402Version int               `json:"x402Version"`
		Scheme      string            `json:"scheme"`
		Network     string            `json:"network"`
		Extra       map[string]string `json:"extra"`
	} `json:"kinds"`
}

// Facilitator talks to an x402 facilitator over HTTP.
type Facilitator struct {
	BaseURL string
	Client  *http.Client

	mu        sync.Mutex
	feePayer  string
	fetchedAt time.Time
}

func (f *Facilitator) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: facilitatorTimeout}
}

func (f *Facilitator) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(f.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return f.do(req, out)
}

func (f *Facilitator) do(req *http.Request, out any) error {
	resp, err := f.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	// A rejected payment can come back as a non-2xx status that still carries
	// the JSON reason, so decode before judging the status.
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("facilitator %s: HTTP %d, unreadable reply", req.URL.Path, resp.StatusCode)
	}
	return nil
}

// FeePayer returns the account the facilitator pays network fees from for
// network. The answer is cached for supportedTTL.
func (f *Facilitator) FeePayer(ctx context.Context, network string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.feePayer != "" && time.Since(f.fetchedAt) < supportedTTL {
		return f.feePayer, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(f.BaseURL, "/")+"/supported", nil)
	if err != nil {
		return "", err
	}
	var s supportedResponse
	if err := f.do(req, &s); err != nil {
		return "", err
	}
	for _, k := range s.Kinds {
		if k.Scheme == schemeExact && k.Network == network && k.Extra["feePayer"] != "" {
			f.feePayer, f.fetchedAt = k.Extra["feePayer"], time.Now()
			return f.feePayer, nil
		}
	}
	return "", fmt.Errorf("facilitator does not support the exact scheme on %s", network)
}

// Verify asks the facilitator whether the payment is valid. It does not settle.
func (f *Facilitator) Verify(ctx context.Context, p PaymentPayload, r PaymentRequirements) (verifyResponse, error) {
	var out verifyResponse
	err := f.post(ctx, "/verify", facilitatorRequest{X402Version: x402Version, PaymentPayload: p, PaymentRequirements: r}, &out)
	return out, err
}

// Settle asks the facilitator to submit the payment.
func (f *Facilitator) Settle(ctx context.Context, p PaymentPayload, r PaymentRequirements) (SettlementResponse, error) {
	var out SettlementResponse
	err := f.post(ctx, "/settle", facilitatorRequest{X402Version: x402Version, PaymentPayload: p, PaymentRequirements: r}, &out)
	return out, err
}

// PaywallConfig is what an operator sets to run a paid relay.
type PaywallConfig struct {
	Facilitator *Facilitator
	Network     string
	Asset       string
	PayTo       string
	// PricePerMiB is the price of one MiB of data-relay traffic in the
	// asset's smallest unit.
	PricePerMiB int64
	// MaxSessionBytes caps what one payment can buy.
	MaxSessionBytes int64
	// PublicURL names the relay in the 402 response, without a path.
	PublicURL string
}

// Paywall serves the paid routes and mints and redeems payment tokens.
type Paywall struct {
	PaywallConfig

	secret []byte
	now    func() time.Time

	mu   sync.Mutex
	used map[string]time.Time
}

// NewPaywall loads or creates the token secret in stateDir, so a relay restart
// does not strand a token that was already paid for.
func NewPaywall(c PaywallConfig, stateDir string) (*Paywall, error) {
	if c.Facilitator == nil || c.PayTo == "" || c.Asset == "" || c.Network == "" || c.PricePerMiB <= 0 || c.MaxSessionBytes <= 0 {
		return nil, errors.New("paid mode needs a facilitator, network, asset, pay-to account, a price and a session cap")
	}
	secret, err := loadOrCreateSecret(filepath.Join(stateDir, "x402-token-secret"))
	if err != nil {
		return nil, err
	}
	return &Paywall{PaywallConfig: c, secret: secret, now: time.Now, used: map[string]time.Time{}}, nil
}

func loadOrCreateSecret(path string) ([]byte, error) {
	if raw, err := os.ReadFile(path); err == nil && len(raw) == 32 {
		return raw, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

// Amount returns the price of bytes of data-relay traffic, rounded up. It is
// never zero.
func (p *Paywall) Amount(bytes int64) int64 {
	return max(1, (bytes*p.PricePerMiB+bytesPerMiB-1)/bytesPerMiB)
}

func (p *Paywall) requirements(ctx context.Context, bytes int64) (PaymentRequirements, error) {
	feePayer, err := p.Facilitator.FeePayer(ctx, p.Network)
	if err != nil {
		return PaymentRequirements{}, err
	}
	return PaymentRequirements{
		Scheme: schemeExact, Network: p.Network, Amount: strconv.FormatInt(p.Amount(bytes), 10),
		Asset: p.Asset, PayTo: p.PayTo, MaxTimeoutSeconds: 180,
		Extra: map[string]string{"feePayer": feePayer},
	}, nil
}

type tokenClaims struct {
	Nonce   string `json:"n"`
	Bytes   int64  `json:"b"`
	Expires int64  `json:"e"`
}

func (p *Paywall) mint(bytes int64) (string, int64, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", 0, err
	}
	exp := p.now().Add(tokenTTL).Unix()
	raw, err := json.Marshal(tokenClaims{Nonce: hex.EncodeToString(nonce[:]), Bytes: bytes, Expires: exp})
	if err != nil {
		return "", 0, err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), exp, nil
}

var errBadToken = errors.New("payment token is missing, invalid, expired or already used")

// Redeem checks a token and spends it. It returns the byte count the token
// paid for. A token works once.
func (p *Paywall) Redeem(token string) (int64, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return 0, errBadToken
	}
	want, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return 0, errBadToken
	}
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(body))
	if !hmac.Equal(mac.Sum(nil), want) {
		return 0, errBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return 0, errBadToken
	}
	var c tokenClaims
	if err := json.Unmarshal(raw, &c); err != nil || c.Bytes <= 0 {
		return 0, errBadToken
	}
	now := p.now()
	if now.Unix() > c.Expires {
		return 0, errBadToken
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for n, exp := range p.used {
		if now.After(exp) {
			delete(p.used, n)
		}
	}
	if _, spent := p.used[c.Nonce]; spent {
		return 0, errBadToken
	}
	p.used[c.Nonce] = time.Unix(c.Expires, 0)
	return c.Bytes, nil
}

// Handler serves the paid routes.
func (p *Paywall) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /x402/session", p.handleSession)
	return mux
}

type sessionTokenReply struct {
	Token       string `json:"token"`
	Bytes       int64  `json:"bytes"`
	ExpiresAt   int64  `json:"expiresAt"`
	Transaction string `json:"transaction"`
}

func (p *Paywall) handleSession(w http.ResponseWriter, r *http.Request) {
	bytes := p.MaxSessionBytes
	if v := r.URL.Query().Get("bytes"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			http.Error(w, `{"error":"bytes must be a positive integer"}`, http.StatusBadRequest)
			return
		}
		bytes = n
	}
	if bytes > p.MaxSessionBytes {
		bytes = p.MaxSessionBytes
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*facilitatorTimeout)
	defer cancel()
	req, err := p.requirements(ctx, bytes)
	if err != nil {
		tracef("x402: no requirements: %v", err)
		http.Error(w, `{"error":"payment service unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	resource := ResourceInfo{URL: p.PublicURL + "/x402/session", Description: "Bramble data-relay session", MimeType: "application/json"}
	header := r.Header.Get(headerSignature)
	if header == "" {
		p.requirePayment(w, resource, req, "")
		return
	}
	if len(header) > maxPaymentHeader {
		http.Error(w, `{"error":"payment header too large"}`, http.StatusBadRequest)
		return
	}
	var payload PaymentPayload
	if err := decodeHeader(header, &payload); err != nil || payload.X402Version != x402Version || payload.Payload.Transaction == "" {
		http.Error(w, `{"error":"invalid payment payload"}`, http.StatusBadRequest)
		return
	}
	// The relay's own requirements decide what is valid. A payload that
	// accepted anything else is refused, and so is a stale price.
	if !sameRequirements(payload.Accepted, req) {
		p.requirePayment(w, resource, req, "payment requirements changed")
		return
	}
	payload.Accepted = req
	v, err := p.Facilitator.Verify(ctx, payload, req)
	if err != nil {
		tracef("x402: verify failed: %v", err)
		http.Error(w, `{"error":"payment service unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if !v.IsValid {
		p.requirePayment(w, resource, req, safeReason(v.InvalidReason))
		return
	}
	s, err := p.Facilitator.Settle(ctx, payload, req)
	if err != nil {
		tracef("x402: settle failed: %v", err)
		http.Error(w, `{"error":"payment service unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if enc, err := encodeHeader(s); err == nil {
		w.Header().Set(headerResponse, enc)
	}
	if !s.Success {
		p.requirePayment(w, resource, req, safeReason(s.ErrorReason))
		return
	}
	token, exp, err := p.mint(bytes)
	if err != nil {
		http.Error(w, `{"error":"could not issue a token"}`, http.StatusInternalServerError)
		return
	}
	tracef("x402: paid %s %s for %d bytes (%s)", req.Amount, req.Asset, bytes, s.Transaction)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessionTokenReply{Token: token, Bytes: bytes, ExpiresAt: exp, Transaction: s.Transaction})
}

func (p *Paywall) requirePayment(w http.ResponseWriter, res ResourceInfo, req PaymentRequirements, reason string) {
	enc, err := encodeHeader(PaymentRequired{X402Version: x402Version, Error: reason, Resource: res, Accepts: []PaymentRequirements{req}})
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set(headerRequired, enc)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	_, _ = w.Write([]byte(`{}`))
}

// safeReason passes on a facilitator reason only when it is a short code, so
// upstream text never reaches a client.
func safeReason(s string) string {
	if s == "" || len(s) > 64 {
		return "payment_failed"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return "payment_failed"
		}
	}
	return s
}

func sameRequirements(a, b PaymentRequirements) bool {
	return a.Scheme == b.Scheme && a.Network == b.Network && a.Amount == b.Amount && a.Asset == b.Asset &&
		a.PayTo == b.PayTo && a.Extra["feePayer"] == b.Extra["feePayer"]
}

func encodeHeader(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func decodeHeader(s string, v any) error {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
