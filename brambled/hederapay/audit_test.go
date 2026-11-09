package hederapay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func ev() Event {
	return Event{"tailnet": "t.net.bramble.eth", "gateway": "server", "requester": "laptop", "service": "web", "outcome": "allowed", "at": "1700000000"}
}

func TestACommitmentOpensOnlyToItsOwnEventAndSalt(t *testing.T) {
	o, err := NewOpening(ev())
	if err != nil {
		t.Fatal(err)
	}
	c, err := o.Recompute()
	if err != nil || c != o.Commitment || len(c) != 64 {
		t.Fatalf("recompute %q %v", c, err)
	}
	other := o
	other.Event = ev()
	other.Event["outcome"] = "denied"
	if c2, _ := other.Recompute(); c2 == c {
		t.Fatal("a different outcome opened the same commitment")
	}
	again, _ := NewOpening(ev())
	if again.Commitment == c {
		t.Fatal("equal events with fresh salts gave equal commitments")
	}
	bad := o
	bad.Salt = "00"
	if _, err := bad.Recompute(); err == nil {
		t.Fatal("a short salt was accepted")
	}
}

func TestACommitmentIsBoundToItsKind(t *testing.T) {
	salt := make([]byte, 32)
	if Commit(salt, "decision", ev()) == Commit(salt, "grant", ev()) {
		t.Fatal("a commitment for one kind opened as another")
	}
}

func TestPublisherPostsOnlyCommitmentsAndKeepsOpenings(t *testing.T) {
	var got atomic.Value
	p := newPublisher("0.0.9", func(_ context.Context, msg []byte) error { got.Store(string(msg)); return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	p.Record(ev())
	deadline := time.Now().Add(5 * time.Second)
	for got.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	msg, _ := got.Load().(string)
	for _, secret := range []string{"laptop", "server", "web", "allowed", "t.net.bramble.eth"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("the posted message leaks %q: %s", secret, msg)
		}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(msg), &m); err != nil || len(m) != 3 || m["kind"] != "decision" || m["v"] != float64(1) {
		t.Fatalf("message %s", msg)
	}
	if o := p.Openings(); len(o) != 1 || o[0].Commitment != m["c"] {
		t.Fatalf("openings %+v", o)
	}
}

func TestPublisherRateLimitsPostsButKeepsEveryOpening(t *testing.T) {
	p := newPublisher("0.0.9", func(context.Context, []byte) error { return nil }) // not running: queue only
	for i := 0; i < perMinute+10; i++ {
		p.Record(ev())
	}
	st := p.Stats()
	if st.Dropped != 10 || len(p.Openings()) != perMinute+10 {
		t.Fatalf("dropped %d, openings %d", st.Dropped, len(p.Openings()))
	}
}

func TestFindCommitmentReadsTheTopicFromTheMirrorNode(t *testing.T) {
	o, _ := NewOpening(ev())
	other, _ := NewOpening(ev())
	enc := func(c string) string {
		raw, _ := json.Marshal(message{Version: 1, Kind: DecisionKind, C: c})
		return base64.StdEncoding.EncodeToString(raw)
	}
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/topics/0.0.9/messages") {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]any{
			{"message": enc(other.Commitment), "sequence_number": 1, "consensus_timestamp": "1.1"},
			{"message": "not base64!", "sequence_number": 2},
			{"message": enc(o.Commitment), "sequence_number": 3, "consensus_timestamp": "1.3"},
		}})
	}))
	defer mirror.Close()
	e, err := FindCommitment(context.Background(), mirror.Client(), mirror.URL, "0.0.9", o.Commitment)
	if err != nil || e == nil || e.Sequence != 3 {
		t.Fatalf("found %+v, %v", e, err)
	}
	missing, _ := NewOpening(ev())
	if e, err := FindCommitment(context.Background(), mirror.Client(), mirror.URL, "0.0.9", missing.Commitment); err != nil || e != nil {
		t.Fatalf("found a commitment that is not there: %+v %v", e, err)
	}
}
