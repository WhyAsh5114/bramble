package rendezvous

import (
	"encoding/json"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// startHostileRelay behaves exactly like the real relay/main.go for every
// pubkey except victimPub: any offer addressed to victimPub gets a forged
// response synthesized on the spot — "from" victimPub, "candidate"
// poisonedCandidate — without victimPub ever being registered or involved at
// all. This is the strong form of Gate 1.2's "advertises an unauthorized peer
// as legitimate": the relay doesn't just mis-forward a real message, it
// fabricates one outright. Models an attacker running their own modified
// relay binary — never a change to the real, dumb relay — per
// docs/adr/0003-rendezvous-relay-split.md's "Trust boundary" section.
func startHostileRelay(t *testing.T, victimPub, poisonedCandidate string) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

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
						if m.To == victimPub {
							// Forged: no lookup, no real victim involved at
							// all — the relay just fabricates a response.
							_ = send(self, message{Type: "offer", From: victimPub, Candidate: poisonedCandidate})
							_ = send(self, message{Type: "ack"})
							continue
						}
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

// TestGate1_2_MaliciousRelayCannotAdmitPeer is Gate 1.2's own test
// (docs/05_BUILD_PLAN.md): "Run a deliberately hostile signaling relay that
// advertises an unauthorized peer as legitimate ... assert the connection is
// refused."
//
// Alice wants to reach Bob, whose real public key she already knows (as ENS
// would supply it, independently of the relay). The relay she uses is
// hostile: it tells her Bob is reachable at the attacker's address instead of
// Bob's real one. The attacker isn't a passive stub either — it's a fully
// live wgnode.Node, actually listening, actually willing to attempt a
// handshake. None of that matters: Alice's handshake is addressed to Bob's
// public key, and Noise_IK only completes for whoever holds the matching
// private key. The attacker doesn't, so admission is refused — proving the
// ADR's "can't make that lie succeed" claim with real code, not just prose.
func TestGate1_2_MaliciousRelayCannotAdmitPeer(t *testing.T) {
	alicePriv, alicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating alice's key pair: %v", err)
	}
	_, bobPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating bob's key pair: %v", err)
	}
	attackerPriv, _, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating attacker's key pair: %v", err)
	}

	aliceAddr := netip.MustParseAddr("10.97.0.1")
	bobAddr := netip.MustParseAddr("10.97.0.2") // never actually stood up — bob is never involved once the relay lies
	attackerAddr := netip.MustParseAddr("10.97.0.3")
	const attackerCandidate = "127.0.0.1:61931"

	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: 61930, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting alice: %v", err)
	}
	defer alice.Close()

	// The attacker: a real, live, responsive WireGuard node — not a stub —
	// standing at the address the hostile relay will substitute for Bob's.
	attacker, err := wgnode.New(wgnode.Config{PrivateKeyHex: attackerPriv, ListenPort: 61931, LocalAddress: attackerAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting attacker: %v", err)
	}
	defer attacker.Close()

	relayAddr := startHostileRelay(t, bobPub, attackerCandidate)

	// Alice runs the real client exchange, asking for bob's candidate. The
	// hostile relay hands her the attacker's address instead.
	learnedCandidate, err := Exchange(relayAddr, alicePub, bobPub, "127.0.0.1:61930", "", 5*time.Second)
	if err != nil {
		t.Fatalf("alice's candidate exchange failed: %v", err)
	}
	if learnedCandidate != attackerCandidate {
		t.Fatalf("test setup broken: expected the hostile relay to hand back %q, got %q", attackerCandidate, learnedCandidate)
	}

	poisonedEndpoint, err := netip.ParseAddrPort(learnedCandidate)
	if err != nil {
		t.Fatalf("parsing poisoned candidate: %v", err)
	}
	// Alice still addresses her peer-table entry to Bob's real public key —
	// that came from ENS, not the relay. Only the endpoint is poisoned.
	if err := alice.AddPeer(bobPub, netip.PrefixFrom(bobAddr, 32), &poisonedEndpoint); err != nil {
		t.Fatalf("alice registering bob (poisoned endpoint): %v", err)
	}

	if pingUntilConnected(t, alice, bobAddr, 8*time.Second) {
		t.Fatal("alice completed a handshake with bob's address despite the relay substituting the attacker's endpoint")
	}

	peers, err := alice.Peers()
	if err != nil {
		t.Fatalf("reading alice's peer table: %v", err)
	}
	for _, p := range peers {
		if p.PublicKeyHex == bobPub && p.Handshaked() {
			t.Fatal("alice recorded a handshake for bob's public key, but the attacker (not bob) answered")
		}
	}
}
