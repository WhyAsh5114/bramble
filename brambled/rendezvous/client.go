// Package rendezvous is the brambled-side client for the dumb rendezvous
// relay (../../relay). The two only share a wire protocol — newline-
// delimited JSON over TCP — not Go types; the relay module is intentionally
// not a dependency of this one, matching its role as a permissionless,
// commodity, opaque-blob forwarder (docs/adr/0003).
package rendezvous

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type message struct {
	Type      string `json:"type"`
	Pubkey    string `json:"pubkey,omitempty"`
	To        string `json:"to,omitempty"`
	From      string `json:"from,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	Message   string `json:"message,omitempty"`
	Token     string `json:"token,omitempty"`
}

// Exchange connects to the relay at addr, registers ownPubkey, publishes
// myCandidate for peerPubkey, and returns peerPubkey's matching candidate
// once the peer performs the same exchange from its side — or an error if
// that doesn't happen within timeout.
//
// token is presented in hello — one rendezvous-token per Exchange call, not
// per wire-level offer retry below, precisely because of that retry
// behavior (see docs/adr/0007). Pass "" against an unmetered relay; a
// metered relay's own tokenVerifier treats an empty token exactly like any
// other invalid one.
//
// The offer is resent every second until the relay acks it: the two sides
// won't generally call this at the exact same instant, and the relay replies
// with an immediate error (not a queued retry) if the target isn't
// registered yet. Exchange does not return as soon as it learns the peer's
// candidate — it also waits for its own offer to be acked, so it can't
// vanish mid-retry and strand a peer whose own first attempt raced ahead of
// this side's registration (see relay/main.go's protocol comment).
func Exchange(addr, ownPubkey, peerPubkey, myCandidate, token string, timeout time.Duration) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return "", fmt.Errorf("dialing relay %s: %w", addr, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return "", err
	}

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(message{Type: "hello", Pubkey: ownPubkey, Token: token}); err != nil {
		return "", fmt.Errorf("registering with relay: %w", err)
	}

	sendOffer := func() error {
		return enc.Encode(message{Type: "offer", To: peerPubkey, Candidate: myCandidate})
	}
	if err := sendOffer(); err != nil {
		return "", fmt.Errorf("sending offer: %w", err)
	}

	type event struct {
		kind      string // "offer", "ack", or "err"
		candidate string
		err       error
	}
	events := make(chan event)
	go func() {
		for {
			var m message
			if err := dec.Decode(&m); err != nil {
				events <- event{kind: "err", err: fmt.Errorf("waiting for %s's candidate via relay: %w", peerPubkey, err)}
				return
			}
			switch {
			case m.Type == "offer" && m.From == peerPubkey:
				events <- event{kind: "offer", candidate: m.Candidate}
			case m.Type == "ack":
				events <- event{kind: "ack"}
			}
			// An "error" reply to a too-early offer isn't fatal — the retry
			// loop below re-sends until acked or the deadline expires.
		}
	}()

	var peerCandidate string
	var haveOffer, ownOfferAcked bool

	retry := time.NewTicker(time.Second)
	defer retry.Stop()
	for {
		if haveOffer && ownOfferAcked {
			return peerCandidate, nil
		}
		select {
		case ev := <-events:
			switch ev.kind {
			case "offer":
				haveOffer, peerCandidate = true, ev.candidate
			case "ack":
				ownOfferAcked = true
			case "err":
				return "", ev.err
			}
		case <-retry.C:
			if time.Now().After(deadline) {
				return "", fmt.Errorf("timed out waiting for %s's candidate via relay", peerPubkey)
			}
			if !ownOfferAcked {
				_ = sendOffer()
			}
		}
	}
}
