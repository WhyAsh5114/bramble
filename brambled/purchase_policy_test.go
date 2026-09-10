package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WhyAsh5114/bramble/brambled/datarelay"
)

func TestValidateDataRelayQuote(t *testing.T) {
	policy := dataRelayPurchasePolicy{MaxPricePerByte: 10, MaxSessionCost: 100}
	tests := []struct {
		name    string
		price   int64
		bytes   int64
		wantErr bool
	}{
		{name: "within both limits", price: 10, bytes: 10},
		{name: "per-byte limit", price: 11, bytes: 1, wantErr: true},
		{name: "session limit", price: 6, bytes: 20, wantErr: true},
		{name: "zero price", price: 0, bytes: 1, wantErr: true},
		{name: "overflow-sized inputs", price: 1<<62 - 1, bytes: 4, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDataRelayQuote(datarelay.Quote{PricePerByte: tt.price}, tt.bytes, policy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDataRelayQuote() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQuoteDataRelayRejectsUnsafeURLShapes(t *testing.T) {
	for _, sidecarURL := range []string{
		"file:///tmp/price",
		"http://user:pass@example.com",
		"http://example.com?target=internal",
		"http://example.com#fragment",
	} {
		t.Run(sidecarURL, func(t *testing.T) {
			if _, err := quoteDataRelay("relay1", sidecarURL); err == nil {
				t.Fatal("expected unsafe sidecar URL to be rejected")
			}
		})
	}
}

func TestQuoteDataRelayDoesNotFollowRedirects(t *testing.T) {
	redirectFollowed := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectFollowed = true
		_ = json.NewEncoder(w).Encode(map[string]string{"asset": "0.0.429274", "amount": "1"})
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer source.Close()

	if _, err := quoteDataRelay("relay1", source.URL); err == nil {
		t.Fatal("expected redirecting quote endpoint to be rejected")
	}
	if redirectFollowed {
		t.Fatal("quote client followed an untrusted redirect")
	}
}
