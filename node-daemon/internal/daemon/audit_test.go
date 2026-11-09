package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/hederapay"
	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/registry/registrytest"
)

func tailnetWithTopic(t *testing.T, topic string) *registry.Tailnet {
	t.Helper()
	chain := registrytest.New(t, "aud.net.bramble.eth")
	if topic != "" {
		chain.SetTailnetText(registry.KeyTailnetAudit, topic)
	}
	tn, err := registry.NewTailnet(chain.Config(""))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tn.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	return tn
}

func TestAuditAloneGivesTheNodeAKeyButNoPaymentFlags(t *testing.T) {
	d := payDaemon(t, "http://127.0.0.1:1")
	if err := d.SetAudit(true); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.payBal = payBalances{Account: "0.0.4242", HBAR: 100}
	d.tn = tailnetWithTopic(t, "0.0.555")
	args := strings.Join(d.payArgsLocked(), " ")
	d.mu.Unlock()
	for _, want := range []string{"-hedera-account 0.0.4242", "-hedera-no-pay", "-audit-topic 0.0.555"} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q", want)
		}
	}
	if strings.Contains(args, "-pay-max") {
		t.Error("payment caps with payments off")
	}
}

func TestNoAuditFlagWithoutATopicOrWithAuditOff(t *testing.T) {
	d := payDaemon(t, "http://127.0.0.1:1")
	if err := d.SetAudit(true); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.payBal = payBalances{Account: "0.0.4242", HBAR: 100}
	d.tn = tailnetWithTopic(t, "")
	noTopic := strings.Contains(strings.Join(d.payArgsLocked(), " "), "-audit-topic")
	d.mu.Unlock()
	if noTopic {
		t.Fatal("an audit flag with no topic record")
	}
	if err := d.SetAudit(false); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.tn = tailnetWithTopic(t, "0.0.555")
	off := d.payArgsLocked()
	d.mu.Unlock()
	if len(off) != 0 {
		t.Fatal("flags with both opt-ins off")
	}
}

func TestCreatingATopicNeedsAFundedAccount(t *testing.T) {
	d := payDaemon(t, "http://127.0.0.1:1")
	if _, err := d.CreateAuditTopic(); err == nil {
		t.Fatal("a topic was created with no key")
	}
	if err := d.SetAudit(true); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateAuditTopic(); err == nil {
		t.Fatal("a topic was created before the account was funded")
	}
}

func TestAnOpeningVerifiesAgainstTheTailnetTopic(t *testing.T) {
	o, err := hederapay.NewOpening(hederapay.Event{"gateway": "server", "requester": "laptop", "service": "web", "outcome": "allowed", "at": "1"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"v": 1, "kind": "decision", "c": o.Commitment})
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/topics/0.0.555/messages") {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]any{
			{"message": base64.StdEncoding.EncodeToString(raw), "sequence_number": 9, "consensus_timestamp": "5.5"},
		}})
	}))
	defer mirror.Close()
	d := payDaemon(t, mirror.URL)
	d.mu.Lock()
	d.tn = tailnetWithTopic(t, "0.0.555")
	d.mu.Unlock()

	res, err := d.VerifyOpening(context.Background(), o)
	if err != nil || !res.Match || res.Sequence != 9 || res.Topic != "0.0.555" {
		t.Fatalf("verify %+v %v", res, err)
	}
	forged := o
	forged.Event = hederapay.Event{"gateway": "server", "requester": "laptop", "service": "web", "outcome": "denied", "at": "1"}
	if res, err := d.VerifyOpening(context.Background(), forged); err != nil || res.Match {
		t.Fatalf("a forged opening matched: %+v %v", res, err)
	}
}
