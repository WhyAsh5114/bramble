package rendezvous

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

func TestHTTPClientChecksTheRelaysPin(t *testing.T) {
	fac := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"kinds":[{"x402Version":2,"scheme":"exact","network":"hedera:testnet","extra":{"feePayer":"0.0.7000"}}]}`)
	}))
	defer fac.Close()
	l, err := relaysrv.StartLocalPaid(relaysrv.Limits{
		SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 4, SessionsPerHour: 100, PortMin: 52100, PortMax: 52109,
	}, nil, relaysrv.PaywallConfig{
		Facilitator: &relaysrv.Facilitator{BaseURL: fac.URL}, Network: "hedera:testnet", Asset: "0.0.429274",
		PayTo: "0.0.9000", PricePerMiB: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	url := "https://" + l.Addr + "/x402/session?bytes=1024"
	post := func(r Relay) (*http.Response, error) {
		return HTTPClient(r, 5*time.Second).Post(url, "", strings.NewReader(""))
	}

	resp, err := post(Relay{Label: "r", Address: l.Addr, Pin: l.Pin})
	if err != nil {
		t.Fatalf("the pinned relay was refused: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status %d, expected 402", resp.StatusCode)
	}

	if resp, err := post(Relay{Label: "r", Address: l.Addr, Pin: "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}); err == nil {
		resp.Body.Close()
		t.Fatal("a payment route answered a client that expected a different key")
	}
	if resp, err := post(Relay{Label: "r", Address: l.Addr}); err == nil {
		resp.Body.Close()
		t.Fatal("a relay with no published pin was trusted")
	}
}
