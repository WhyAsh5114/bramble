package rendezvous

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeRelay is a minimal stand-in for ../../relay's Server, reimplemented
// here rather than imported: the relay is a separate Go module by design
// (it's a standalone deployable, not a brambled dependency), and the two
// only need to agree on the wire protocol, not share Go types.
func startFakeRelay(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	// A json.Encoder isn't safe for concurrent use, and two different
	// senders' handler goroutines can end up writing to the same target
	// around the same time — mirror relay/main.go's per-peer write mutex
	// rather than risk a flaky, corrupted-stream test failure.
	type fakePeer struct {
		mu  sync.Mutex
		enc *json.Encoder
	}
	send := func(p *fakePeer, m message) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.enc.Encode(m)
	}

	var mu sync.Mutex
	peers := make(map[string]*fakePeer)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				self := &fakePeer{enc: json.NewEncoder(conn)}
				dec := json.NewDecoder(conn)
				var pubkey string
				for {
					var m message
					if err := dec.Decode(&m); err != nil {
						return
					}
					switch m.Type {
					case "hello":
						pubkey = m.Pubkey
						mu.Lock()
						peers[pubkey] = self
						mu.Unlock()
					case "offer":
						mu.Lock()
						target, ok := peers[m.To]
						mu.Unlock()
						if !ok {
							_ = send(self, message{Type: "error", Message: "peer not registered"})
							continue
						}
						_ = send(target, message{Type: "offer", From: pubkey, Candidate: m.Candidate})
						_ = send(self, message{Type: "ack"})
					}
				}
			}()
		}
	}()

	return ln.Addr().String()
}

func TestExchangeBothSidesCallSimultaneously(t *testing.T) {
	addr := startFakeRelay(t)

	type outcome struct {
		candidate string
		err       error
	}
	aliceCh := make(chan outcome, 1)
	bobCh := make(chan outcome, 1)

	go func() {
		c, err := Exchange(addr, "alice-pub", "bob-pub", "10.0.0.1:51820", "", 5*time.Second)
		aliceCh <- outcome{c, err}
	}()
	go func() {
		c, err := Exchange(addr, "bob-pub", "alice-pub", "10.0.0.2:51820", "", 5*time.Second)
		bobCh <- outcome{c, err}
	}()

	alice := <-aliceCh
	bob := <-bobCh

	if alice.err != nil {
		t.Fatalf("alice's exchange failed: %v", alice.err)
	}
	if bob.err != nil {
		t.Fatalf("bob's exchange failed: %v", bob.err)
	}
	if alice.candidate != "10.0.0.2:51820" {
		t.Fatalf("alice got wrong candidate for bob: %q", alice.candidate)
	}
	if bob.candidate != "10.0.0.1:51820" {
		t.Fatalf("bob got wrong candidate for alice: %q", bob.candidate)
	}
}

func TestExchangeRetriesUntilPeerArrives(t *testing.T) {
	addr := startFakeRelay(t)

	resultCh := make(chan struct {
		candidate string
		err       error
	}, 1)
	go func() {
		c, err := Exchange(addr, "alice-pub", "bob-pub", "10.0.0.1:51820", "", 5*time.Second)
		resultCh <- struct {
			candidate string
			err       error
		}{c, err}
	}()

	// Bob shows up late — after the relay has already told alice "not
	// registered" at least once. Exchange must retry rather than giving up.
	time.Sleep(1500 * time.Millisecond)
	go func() {
		_, _ = Exchange(addr, "bob-pub", "alice-pub", "10.0.0.2:51820", "", 5*time.Second)
	}()

	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("alice's exchange failed: %v", r.err)
		}
		if r.candidate != "10.0.0.2:51820" {
			t.Fatalf("alice got wrong candidate for bob: %q", r.candidate)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for alice's exchange to succeed after bob's late arrival")
	}
}

func TestExchangeTimesOutIfPeerNeverArrives(t *testing.T) {
	addr := startFakeRelay(t)

	_, err := Exchange(addr, "alice-pub", "nobody-pub", "10.0.0.1:51820", "", 2*time.Second)
	if err == nil {
		t.Fatal("expected a timeout error when the peer never registers")
	}
}
