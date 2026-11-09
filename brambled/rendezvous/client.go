package rendezvous

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Relay is a rendezvous endpoint and the TLS key pin published for it in the
// relay directory (registry.KeyPin).
type Relay struct {
	Label   string
	Address string
	Pin     string
}

type message struct {
	Type      string        `json:"type"`
	Pubkey    string        `json:"pubkey,omitempty"`
	To        string        `json:"to,omitempty"`
	From      string        `json:"from,omitempty"`
	Candidate string        `json:"candidate,omitempty"`
	Message   string        `json:"message,omitempty"`
	Bytes     int64         `json:"bytes,omitempty"`
	Session   *SessionGrant `json:"session,omitempty"`
	Status    *RelayStatus  `json:"status,omitempty"`
}

// SessionGrant is a data-relay session the relay allocated.
type SessionGrant struct {
	ID        string `json:"id"`
	Endpoint  string `json:"endpoint"`
	Secret    string `json:"secret"`
	Bytes     int64  `json:"bytes"`
	ExpiresAt int64  `json:"expiresAt"`
}

// RelayStatus is a relay's self-reported load and budget.
type RelayStatus struct {
	Version  string `json:"version"`
	Peers    int    `json:"peers"`
	Sessions int    `json:"sessions"`
	Free     bool   `json:"free"`
	Budget   struct {
		DayBytes         int64 `json:"dayBytes"`
		DayLimit         int64 `json:"dayLimit"`
		MonthBytes       int64 `json:"monthBytes"`
		MonthLimit       int64 `json:"monthLimit"`
		ForwardingPaused bool  `json:"forwardingPaused"`
	} `json:"budget"`
}

// AllowUnpinnedForTests lets tests talk to a relay with a throwaway key.
var AllowUnpinnedForTests = false

// pinOf returns "sha256/<base64url SPKI hash>" of a certificate's key.
func pinOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256/" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// dial opens a TLS connection to the relay and checks its key against the pin.
func dial(r Relay, timeout time.Duration) (*tls.Conn, error) {
	if r.Pin == "" && !AllowUnpinnedForTests {
		return nil, fmt.Errorf("relay %s has no published pin; refusing to talk to an unauthenticated relay", r.Label)
	}
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"bramble-relay/1"},
		InsecureSkipVerify: true, //nolint:gosec // the key is checked against the published pin below
		VerifyConnection: func(cs tls.ConnectionState) error {
			if AllowUnpinnedForTests && r.Pin == "" {
				return nil
			}
			if len(cs.PeerCertificates) == 0 || pinOf(cs.PeerCertificates[0]) != r.Pin {
				return errors.New("relay key does not match its published pin")
			}
			return nil
		},
	}
	d := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(d, "tcp", r.Address, cfg)
	if err != nil {
		return nil, fmt.Errorf("dialing relay %s: %w", r.Address, err)
	}
	return conn, nil
}

type wire struct {
	conn net.Conn
	enc  *json.Encoder
	r    *bufio.Reader
}

func open(r Relay, timeout time.Duration) (*wire, error) {
	conn, err := dial(r, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	return &wire{conn: conn, enc: json.NewEncoder(conn), r: bufio.NewReader(conn)}, nil
}

func (w *wire) recv() (message, error) {
	line, err := w.r.ReadBytes('\n')
	if err != nil {
		return message{}, err
	}
	var m message
	err = json.Unmarshal(line, &m)
	return m, err
}

// Exchange swaps candidates with peerPubkey through relay. The candidates are
// sealed end to end; the relay only sees pseudonymous IDs. ownPubkey must have
// been registered with RegisterIdentity.
func Exchange(relay Relay, ownPubkey, peerPubkey, myCandidate string, timeout time.Duration) (string, error) {
	keys, err := derivePair(ownPubkey, peerPubkey)
	if err != nil {
		return "", err
	}
	w, err := open(relay, timeout)
	if err != nil {
		return "", err
	}
	defer w.conn.Close()
	deadline := time.Now().Add(timeout)

	if err := w.enc.Encode(message{Type: "hello", Pubkey: keys.myID, To: keys.peerID}); err != nil {
		return "", fmt.Errorf("registering with relay: %w", err)
	}
	sendOffer := func() error {
		sealed, err := keys.seal(myCandidate, time.Now())
		if err != nil {
			return err
		}
		return w.enc.Encode(message{Type: "offer", To: keys.peerID, Candidate: sealed})
	}
	if err := sendOffer(); err != nil {
		return "", fmt.Errorf("sending offer: %w", err)
	}

	type event struct {
		kind      string
		candidate string
		err       error
	}
	events := make(chan event, 4)
	go func() {
		for {
			m, err := w.recv()
			if err != nil {
				events <- event{kind: "err", err: fmt.Errorf("waiting for %s's candidate via relay: %w", short(peerPubkey), err)}
				return
			}
			switch {
			case m.Type == "offer" && m.From == keys.peerID:
				c, err := keys.open(m.Candidate, time.Now())
				if err != nil {
					continue // forged, replayed or garbled: ignore and keep waiting
				}
				events <- event{kind: "offer", candidate: c}
			case m.Type == "ack":
				events <- event{kind: "ack"}
			}
		}
	}()

	var peerCandidate string
	var haveOffer, acked bool
	retry := time.NewTicker(time.Second)
	defer retry.Stop()
	for {
		if haveOffer && acked {
			return peerCandidate, nil
		}
		select {
		case ev := <-events:
			switch ev.kind {
			case "offer":
				haveOffer, peerCandidate = true, ev.candidate
				if !acked {
					_ = sendOffer()
				}
			case "ack":
				acked = true
			case "err":
				return "", ev.err
			}
		case <-retry.C:
			if time.Now().After(deadline) {
				return "", fmt.Errorf("timed out waiting for %s's candidate via relay", short(peerPubkey))
			}
			if !acked {
				_ = sendOffer()
			}
		}
	}
}

// RequestSession asks relay for a data-relay session of up to bytes.
func RequestSession(relay Relay, bytes int64, timeout time.Duration) (*SessionGrant, error) {
	w, err := open(relay, timeout)
	if err != nil {
		return nil, err
	}
	defer w.conn.Close()
	if err := w.enc.Encode(message{Type: "session", Bytes: bytes}); err != nil {
		return nil, err
	}
	m, err := w.recv()
	if err != nil {
		return nil, err
	}
	if m.Type != "session" || m.Session == nil {
		return nil, fmt.Errorf("relay %s refused a session: %s", relay.Label, strings.TrimSpace(m.Message))
	}
	return m.Session, nil
}

// Status asks relay for its load and budget.
func Status(relay Relay, timeout time.Duration) (*RelayStatus, error) {
	w, err := open(relay, timeout)
	if err != nil {
		return nil, err
	}
	defer w.conn.Close()
	if err := w.enc.Encode(message{Type: "status"}); err != nil {
		return nil, err
	}
	m, err := w.recv()
	if err != nil {
		return nil, err
	}
	if m.Status == nil {
		return nil, fmt.Errorf("relay %s: no status", relay.Label)
	}
	return m.Status, nil
}
