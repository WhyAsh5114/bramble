package relaysrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// A paid relay can publish a receipt for every session it sells to a Hedera
// Consensus Service topic (ADR 0021). The receipt holds only what the payment
// transaction already shows publicly, plus the byte count: nobody learns who
// talked to whom. The topic has a submit key, so only this relay can write to
// it, which makes the log the relay's own signed record of what it sold.

// Receipt is one sold session.
type Receipt struct {
	Transaction string    `json:"tx"`
	Bytes       int64     `json:"bytes"`
	Amount      string    `json:"amount"`
	Asset       string    `json:"asset"`
	Network     string    `json:"network"`
	At          time.Time `json:"-"`
}

// ReceiptSink takes receipts. Submit never blocks the session that caused it.
type ReceiptSink interface {
	Submit(Receipt)
}

type receiptMessage struct {
	Version int    `json:"v"`
	Kind    string `json:"kind"`
	Receipt
	At int64 `json:"at"`
}

func encodeReceipt(r Receipt) ([]byte, error) {
	return json.Marshal(receiptMessage{Version: 1, Kind: "relay-session", Receipt: r, At: r.At.Unix()})
}

// HCSAuditor writes receipts to an HCS topic in the background.
type HCSAuditor struct {
	// Topic is the topic ID, like 0.0.1234.
	Topic string

	submit func(ctx context.Context, msg []byte) error
	q      chan Receipt
	wg     sync.WaitGroup
}

const (
	auditQueue   = 256
	auditRetries = 3
	auditTimeout = 30 * time.Second
	topicFile    = "hcs-topic"
)

func newAuditor(topic string, submit func(context.Context, []byte) error) *HCSAuditor {
	return &HCSAuditor{Topic: topic, submit: submit, q: make(chan Receipt, auditQueue)}
}

// NewHCSAuditor connects to Hedera testnet as the relay operator. If topic is
// empty it reuses the topic saved in stateDir, or creates one that only the
// operator can write to and saves it. The key is never printed or logged.
func NewHCSAuditor(account, privateKeyHex, topic, stateDir string) (*HCSAuditor, error) {
	acct, err := hiero.AccountIDFromString(account)
	if err != nil {
		return nil, fmt.Errorf("hedera account ID: %w", err)
	}
	key, err := hiero.PrivateKeyFromStringECDSA(privateKeyHex)
	if err != nil {
		return nil, errors.New("hedera private key is not a valid ECDSA key")
	}
	client := hiero.ClientForTestnet()
	client.SetOperator(acct, key)

	path := filepath.Join(stateDir, topicFile)
	if topic == "" {
		if raw, err := os.ReadFile(path); err == nil {
			topic = strings.TrimSpace(string(raw))
		}
	}
	if topic == "" {
		ctx, cancel := context.WithTimeout(context.Background(), auditTimeout)
		defer cancel()
		if topic, err = createTopic(ctx, client, key.PublicKey()); err != nil {
			return nil, fmt.Errorf("creating the audit topic: %w", err)
		}
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(topic+"\n"), 0o600); err != nil {
			return nil, err
		}
	}
	id, err := hiero.TopicIDFromString(topic)
	if err != nil {
		return nil, fmt.Errorf("audit topic: %w", err)
	}
	return newAuditor(topic, func(ctx context.Context, msg []byte) error {
		tx, err := hiero.NewTopicMessageSubmitTransaction().SetTopicID(id).SetMessage(msg).Execute(client)
		if err != nil {
			return err
		}
		_, err = tx.GetReceipt(client)
		return err
	}), nil
}

func createTopic(ctx context.Context, client *hiero.Client, submitKey hiero.PublicKey) (string, error) {
	tx, err := hiero.NewTopicCreateTransaction().SetTopicMemo("bramble relay receipts").SetSubmitKey(submitKey).Execute(client)
	if err != nil {
		return "", err
	}
	receipt, err := tx.GetReceipt(client)
	if err != nil {
		return "", err
	}
	if receipt.TopicID == nil {
		return "", errors.New("no topic ID in the receipt")
	}
	return receipt.TopicID.String(), nil
}

// Submit queues r. A full queue drops it: a slow or unreachable Hedera must
// never hold up a data session.
func (a *HCSAuditor) Submit(r Receipt) {
	select {
	case a.q <- r:
	default:
		log.Printf("audit: queue full, dropped a receipt for %s", r.Transaction)
	}
}

// Run sends queued receipts until ctx ends.
func (a *HCSAuditor) Run(ctx context.Context) {
	a.wg.Add(1)
	defer a.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-a.q:
			a.send(ctx, r)
		}
	}
}

func (a *HCSAuditor) send(ctx context.Context, r Receipt) {
	msg, err := encodeReceipt(r)
	if err != nil {
		return
	}
	var last error
	for i := 0; i < auditRetries; i++ {
		sctx, cancel := context.WithTimeout(ctx, auditTimeout)
		last = a.submit(sctx, msg)
		cancel()
		if last == nil {
			tracef("audit: receipt for %s on topic %s", r.Transaction, a.Topic)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(i+1) * 2 * time.Second):
		}
	}
	log.Printf("audit: could not write the receipt for %s: %v", r.Transaction, last)
}

// Wait returns once Run has stopped (tests).
func (a *HCSAuditor) Wait() { a.wg.Wait() }
