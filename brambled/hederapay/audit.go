package hederapay

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// Gateway decisions can go on a Hedera Consensus Service topic as salted
// commitments (ADR 0021; the format is the archived one in
// docs/archive/hackathon/16_HCS_AUDIT_RECORD_FORMAT.md). The topic shows that a
// decision happened and when; the device, service and outcome stay with the
// gateway's owner, who can open a commitment to an auditor.

// DecisionKind is the commitment kind for a gateway decision.
const DecisionKind = "decision"

const commitDomain = "bramble-audit-v1"

// Event is one decision's fields. Every value is a string; "kind" is added by
// the commitment.
type Event map[string]string

// Opening is what the gateway keeps and may show an auditor.
type Opening struct {
	Version int    `json:"v"`
	Event   Event  `json:"event"`
	Salt    string `json:"salt"`
	// Commitment is derived, kept for convenience.
	Commitment string `json:"c,omitempty"`
}

func lp(b *[]byte, s string) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(s)))
	*b = append(append(*b, n[:]...), s...)
}

// Commit computes HMAC-SHA256(salt, preimage) in lowercase hex. The kind is
// part of the preimage, so a commitment for one kind never opens as another.
func Commit(salt []byte, kind string, ev Event) string {
	fields := map[string]string{"kind": kind}
	for k, v := range ev {
		fields[k] = v
	}
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	var pre []byte
	lp(&pre, commitDomain)
	for _, k := range names {
		lp(&pre, k)
		lp(&pre, fields[k])
	}
	mac := hmac.New(sha256.New, salt)
	mac.Write(pre)
	return hex.EncodeToString(mac.Sum(nil))
}

// NewOpening commits to ev with a fresh salt.
func NewOpening(ev Event) (Opening, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return Opening{}, err
	}
	return Opening{Version: 1, Event: ev, Salt: hex.EncodeToString(salt), Commitment: Commit(salt, DecisionKind, ev)}, nil
}

// Recompute checks an opening and returns its commitment.
func (o Opening) Recompute() (string, error) {
	if o.Version != 1 || len(o.Event) == 0 {
		return "", errors.New("not a version 1 opening")
	}
	salt, err := hex.DecodeString(o.Salt)
	if err != nil || len(salt) != 32 {
		return "", errors.New("the salt must be 64 hex characters")
	}
	return Commit(salt, DecisionKind, o.Event), nil
}

type message struct {
	Version int    `json:"v"`
	Kind    string `json:"kind"`
	C       string `json:"c"`
}

const (
	auditQueue   = 128
	auditRetries = 3
	auditTimeout = 30 * time.Second
	// perMinute bounds what one gateway posts: each message costs HBAR.
	perMinute   = 30
	maxOpenings = 200
)

// Publisher posts commitments to a topic in the background and keeps the
// openings in memory. Nothing is written to disk.
type Publisher struct {
	Topic string

	submit func(ctx context.Context, msg []byte) error
	q      chan string

	mu       sync.Mutex
	openings []Opening
	posted   int
	dropped  int
	failed   int
	window   []time.Time
	lastErr  string
}

// AuditStats is what the UI shows.
type AuditStats struct {
	Topic   string `json:"topic"`
	Posted  int    `json:"posted"`
	Dropped int    `json:"dropped"`
	Failed  int    `json:"failed"`
	Error   string `json:"error,omitempty"`
}

func newPublisher(topic string, submit func(context.Context, []byte) error) *Publisher {
	return &Publisher{Topic: topic, submit: submit, q: make(chan string, auditQueue)}
}

func client(account hiero.AccountID, key hiero.PrivateKey) *hiero.Client {
	c := hiero.ClientForTestnet()
	c.SetOperator(account, key)
	return c
}

// NewPublisher posts as the node's own Hedera account, which pays the fee.
func NewPublisher(s *Signer, topic string) (*Publisher, error) {
	id, err := hiero.TopicIDFromString(topic)
	if err != nil {
		return nil, fmt.Errorf("audit topic: %w", err)
	}
	c := client(s.account, s.key)
	return newPublisher(topic, func(ctx context.Context, msg []byte) error {
		tx, err := hiero.NewTopicMessageSubmitTransaction().SetTopicID(id).SetMessage(msg).Execute(c)
		if err != nil {
			return err
		}
		_, err = tx.GetReceipt(c)
		return err
	}), nil
}

// CreateTopic makes a topic anyone in the tailnet's gateways may post to. It
// has no admin key, so nobody can delete it or change its memo, and no submit
// key, so every gateway can write. Anyone could also post noise; that costs
// them HBAR and cannot match an opening.
func CreateTopic(s *Signer, memo string) (string, error) {
	c := client(s.account, s.key)
	tx, err := hiero.NewTopicCreateTransaction().SetTopicMemo(memo).Execute(c)
	if err != nil {
		return "", err
	}
	r, err := tx.GetReceipt(c)
	if err != nil {
		return "", err
	}
	if r.TopicID == nil {
		return "", errors.New("no topic ID in the receipt")
	}
	return r.TopicID.String(), nil
}

// Record commits to ev, keeps the opening and queues the commitment. A queue
// that is full, or more than perMinute decisions a minute, drops the post but
// not the opening. It never blocks the caller.
func (p *Publisher) Record(ev Event) {
	o, err := NewOpening(ev)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.openings = append(p.openings, o)
	if len(p.openings) > maxOpenings {
		p.openings = p.openings[len(p.openings)-maxOpenings:]
	}
	now := time.Now()
	keep := p.window[:0]
	for _, t := range p.window {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	p.window = keep
	if len(p.window) >= perMinute {
		p.dropped++
		p.mu.Unlock()
		return
	}
	p.window = append(p.window, now)
	p.mu.Unlock()
	select {
	case p.q <- o.Commitment:
	default:
		p.mu.Lock()
		p.dropped++
		p.mu.Unlock()
	}
}

// Run posts queued commitments until ctx ends.
func (p *Publisher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-p.q:
			p.post(ctx, c)
		}
	}
}

func (p *Publisher) post(ctx context.Context, c string) {
	msg, _ := json.Marshal(message{Version: 1, Kind: DecisionKind, C: c})
	var last error
	for i := 0; i < auditRetries; i++ {
		sctx, cancel := context.WithTimeout(ctx, auditTimeout)
		last = p.submit(sctx, msg)
		cancel()
		if last == nil {
			p.mu.Lock()
			p.posted++
			p.lastErr = ""
			p.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(i+1) * 2 * time.Second):
		}
	}
	p.mu.Lock()
	p.failed++
	p.lastErr = "could not write to the topic; is the account funded with HBAR?"
	p.mu.Unlock()
}

// Openings returns the kept openings, newest last.
func (p *Publisher) Openings() []Opening {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Opening{}, p.openings...)
}

// Stats reports what was posted.
func (p *Publisher) Stats() AuditStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return AuditStats{Topic: p.Topic, Posted: p.posted, Dropped: p.dropped, Failed: p.failed, Error: p.lastErr}
}

// Entry is a commitment found on a topic.
type Entry struct {
	Sequence  int64  `json:"sequence"`
	Timestamp string `json:"consensusTimestamp"`
}

// FindCommitment looks for c on topic through the mirror node at base. It
// reads at most a few pages of the newest messages.
func FindCommitment(ctx context.Context, hc *http.Client, base, topic, c string) (*Entry, error) {
	url := base + "/api/v1/topics/" + topic + "/messages?limit=100&order=desc"
	for page := 0; page < 5 && url != ""; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, errors.New("the Hedera mirror node did not answer")
		}
		var body struct {
			Messages []struct {
				Message   string `json:"message"`
				Sequence  int64  `json:"sequence_number"`
				Timestamp string `json:"consensus_timestamp"`
			} `json:"messages"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20)).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || err != nil {
			return nil, fmt.Errorf("the Hedera mirror node answered %d", resp.StatusCode)
		}
		for _, m := range body.Messages {
			raw, err := base64.StdEncoding.DecodeString(m.Message)
			if err != nil {
				continue
			}
			var msg message
			if json.Unmarshal(raw, &msg) != nil || msg.Version != 1 || msg.Kind != DecisionKind {
				continue
			}
			if hmac.Equal([]byte(msg.C), []byte(c)) {
				return &Entry{Sequence: m.Sequence, Timestamp: m.Timestamp}, nil
			}
		}
		url = ""
		if body.Links.Next != "" {
			url = base + body.Links.Next
		}
	}
	return nil, nil
}
