//go:build live

package relaysrv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Live test: creates an HCS topic on Hedera testnet with the relay operator's
// key and writes one receipt, then reads it back from the public mirror node.
// It costs a fraction of a cent of test HBAR. The key goes straight into the
// SDK and is never printed.
//
//	set -a && . ../../.env && set +a && cd relay && go test -tags live -run TestLive -v ./relaysrv/
func TestLiveReceiptReachesTheTopic(t *testing.T) {
	account, key := os.Getenv("HEDERA_RELAY_ACCOUNT_ID"), os.Getenv("HEDERA_RELAY_PRIVATE_KEY")
	if account == "" || key == "" {
		t.Skip("HEDERA_RELAY_ACCOUNT_ID and HEDERA_RELAY_PRIVATE_KEY are not set")
	}
	dir := t.TempDir()
	a, err := NewHCSAuditor(account, key, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(dir + "/hcs-topic"); err != nil || strings.TrimSpace(string(raw)) != a.Topic {
		t.Fatalf("the topic was not remembered: %v", err)
	}
	// A second start reuses it.
	if b, err := NewHCSAuditor(account, key, "", dir); err != nil || b.Topic != a.Topic {
		t.Fatalf("restart made a new topic: %v", err)
	}
	t.Logf("topic %s", a.Topic)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	a.Submit(Receipt{Transaction: "0.0.7162784@1.000000001", Bytes: 524288, Amount: "500", Asset: "0.0.429274", Network: "hedera:testnet", At: time.Now()})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("https://testnet.mirrornode.hedera.com/api/v1/topics/" + a.Topic + "/messages")
		if err == nil {
			var body struct {
				Messages []struct {
					Message string `json:"message"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if len(body.Messages) > 0 {
				raw, _ := base64.StdEncoding.DecodeString(body.Messages[0].Message)
				t.Logf("on the topic: %s", raw)
				if !strings.Contains(string(raw), `"relay-session"`) {
					t.Fatalf("unexpected message %s", raw)
				}
				return
			}
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatal("the receipt did not reach the topic in 60 s")
}
