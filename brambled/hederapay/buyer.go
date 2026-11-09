package hederapay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Buyer pays a paid relay for data sessions.
type Buyer struct {
	Signer *Signer
	// MaxAmount is the most one payment may cost, per asset, in the asset's
	// smallest unit. A relay that asks for an asset not listed here is never
	// paid. This is the spend cap.
	MaxAmount map[string]int64
}

// Purchase is what a successful payment buys: a single-use token for one data
// session, which the node presents in the rendezvous "session" message.
type Purchase struct {
	Token       string `json:"token"`
	Bytes       int64  `json:"bytes"`
	ExpiresAt   int64  `json:"expiresAt"`
	Transaction string `json:"transaction"`
}

// PaymentError is a payment the relay or the facilitator refused. Reason is a
// short code, never upstream text.
type PaymentError struct{ Reason string }

func (e *PaymentError) Error() string { return "payment refused: " + e.Reason }

const maxBody = 1 << 20

// BuySession pays the relay at baseURL (an https URL, no path) for a data
// session of up to bytes. hc must authenticate the relay, which for a relay
// from the directory means checking its published TLS pin.
func (b *Buyer) BuySession(ctx context.Context, hc *http.Client, baseURL string, bytes int64) (*Purchase, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("a relay is paid over https only")
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/x402/session?bytes=" + strconv.FormatInt(bytes, 10)

	resp, err := post(ctx, hc, endpoint, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusPaymentRequired {
		return nil, fmt.Errorf("relay answered %d, expected 402 Payment Required", resp.StatusCode)
	}
	var required Required
	if err := decodeHeader(resp.Header.Get(headerRequired), &required); err != nil || required.X402Version != x402Version {
		return nil, errors.New("the 402 response carries no readable payment requirements")
	}
	chosen, err := b.choose(required)
	if err != nil {
		return nil, err
	}
	transaction, err := b.Signer.PartialTransfer(chosen)
	if err != nil {
		return nil, err
	}
	payload := Payload{X402Version: x402Version, Resource: required.Resource, Accepted: chosen}
	payload.Payload.Transaction = transaction
	header, err := encodeHeader(payload)
	if err != nil {
		return nil, err
	}

	resp, err = post(ctx, hc, endpoint, header)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusPaymentRequired:
		var again Required
		_ = decodeHeader(resp.Header.Get(headerRequired), &again)
		return nil, &PaymentError{Reason: safeReason(again.Error)}
	default:
		return nil, fmt.Errorf("relay answered %d to a payment", resp.StatusCode)
	}
	var p Purchase
	if err := json.Unmarshal(resp.Body, &p); err != nil || p.Token == "" || p.Bytes <= 0 {
		return nil, errors.New("the relay took payment but sent no usable token")
	}
	return &p, nil
}

// choose picks the first requirement this node is willing to pay: the exact
// scheme, on testnet, in an asset with a spend cap, at or under that cap.
func (b *Buyer) choose(r Required) (Requirements, error) {
	for _, req := range r.Accepts {
		if req.Scheme != schemeExact || req.Network != NetworkTestnet || req.Extra["feePayer"] == "" {
			continue
		}
		limit, ok := b.MaxAmount[req.Asset]
		if !ok {
			continue
		}
		amount, ok := new(big.Int).SetString(req.Amount, 10)
		if !ok || amount.Sign() <= 0 || !amount.IsInt64() {
			continue
		}
		if amount.Int64() > limit {
			return Requirements{}, fmt.Errorf("the relay asks %s of %s, over this node's spend cap of %d", req.Amount, req.Asset, limit)
		}
		return req, nil
	}
	return Requirements{}, errors.New("the relay accepts no payment this node will make")
}

type response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func post(ctx context.Context, hc *http.Client, endpoint, signature string) (*response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if signature != "" {
		req.Header.Set(headerSignature, signature)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	return &response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// safeReason keeps a reason only when it is a short code.
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
