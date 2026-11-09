package rendezvous

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

var loopback = netip.MustParseAddr("127.0.0.1")

// freeUDPPorts returns n distinct UDP ports that were free a moment ago.
// All probe sockets stay open until every port is chosen, so two calls
// cannot return the same port. Fixed ports collide when several agents run
// the suite on one host.
func freeUDPPorts(t *testing.T, n int) []uint16 {
	t.Helper()
	ports := make([]uint16, 0, n)
	for range n {
		c, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("finding a free UDP port: %v", err)
		}
		defer c.Close()
		ports = append(ports, uint16(c.LocalAddr().(*net.UDPAddr).Port))
	}
	return ports
}

// ensStub is the test's stand-in for ENS: the only source of admission.
type ensStub map[string]*registry.DeviceRecord

func (e ensStub) ResolveDevice(label string) (*registry.DeviceRecord, error) { return e[label], nil }

// admits reports whether ENS currently admits pubkey: a pubkey record that
// is not revoked. admission.Loop also checks expiry; this stub does not.
func (e ensStub) admits(pubkey string) bool {
	for _, rec := range e {
		if rec != nil && rec.Pubkey != nil && *rec.Pubkey == pubkey && !rec.Revoked {
			return true
		}
	}
	return false
}

// recordingTable passes AddPeer through to a real wgnode and remembers the
// endpoint each peer was given.
type recordingTable struct {
	node *wgnode.Node
	mu   sync.Mutex
	eps  map[string]*netip.AddrPort
}

func (r *recordingTable) AddPeer(pk string, allowed netip.Prefix, ep *netip.AddrPort) error {
	r.mu.Lock()
	r.eps[pk] = ep
	r.mu.Unlock()
	return r.node.AddPeer(pk, allowed, ep)
}

func (r *recordingTable) RemovePeer(pk string) error { return r.node.RemovePeer(pk) }

// TestEndpointsLearnedOverRendezvousReachAdmittedPeersOnly runs two real
// in-process WireGuard nodes that know nothing about each other except an
// ENS pubkey. They learn each other's STUN-style endpoint over the rendezvous
// wire protocol and complete a handshake. A third party, not admitted in ENS,
// publishes an endpoint for alice at the same relay and is never added.
func TestEndpointsLearnedOverRendezvousReachAdmittedPeersOnly(t *testing.T) {
	relayAddr := startFakeRelay(t)

	alicePriv, alicePub, _ := wgnode.GenerateKeyPair()
	bobPriv, bobPub, _ := wgnode.GenerateKeyPair()
	malloryPriv, malloryPub, _ := wgnode.GenerateKeyPair()
	for _, k := range []string{alicePriv, bobPriv, malloryPriv} {
		if _, err := RegisterIdentity(k); err != nil {
			t.Fatal(err)
		}
	}

	aliceAddr, bobAddr, malloryAddr := netip.MustParseAddr("10.97.0.1"), netip.MustParseAddr("10.97.0.2"), netip.MustParseAddr("10.97.0.3")
	ports := freeUDPPorts(t, 2)
	alicePort, bobPort := ports[0], ports[1]

	alice, err := wgnode.New(wgnode.Config{PrivateKeyHex: alicePriv, ListenPort: alicePort, LocalAddress: aliceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting alice: %v", err)
	}
	defer alice.Close()
	bob, err := wgnode.New(wgnode.Config{PrivateKeyHex: bobPriv, ListenPort: bobPort, LocalAddress: bobAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting bob: %v", err)
	}
	defer bob.Close()

	expiry := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	ens := ensStub{
		"alice":   {Pubkey: &alicePub, Expiry: expiry},
		"bob":     {Pubkey: &bobPub, Expiry: expiry},
		"mallory": {Pubkey: &malloryPub, Expiry: expiry, Revoked: true},
	}

	var callsMu sync.Mutex
	rendezvousCalls := map[string]int{}
	newExchanger := func(own string, ownPort uint16) *Exchanger {
		return &Exchanger{
			Own: own,
			// A real node would publish its STUN-discovered address here.
			Local: func() Announcement {
				return Announcement{{Kind: KindSTUN, Addr: netip.AddrPortFrom(loopback, ownPort)}}
			},
			Via: func(o, p, c string) (string, error) {
				callsMu.Lock()
				rendezvousCalls[p]++
				callsMu.Unlock()
				return Exchange(relayAddr, o, p, c, 5*time.Second)
			},
			Admitted:      ens.admits,
			AllowLoopback: true,
		}
	}

	newLoop := func(table *recordingTable, ex *Exchanger, peers ...admission.Peer) *admission.Loop {
		return &admission.Loop{
			Resolver: ens,
			Table:    table,
			Peers:    peers,
			TTL:      time.Hour,
			EndpointResolver: func(_ admission.Peer, pk string) (*netip.AddrPort, error) {
				learned, err := ex.Learn(pk)
				if err != nil {
					return nil, err
				}
				best, ok := learned.Best()
				if !ok {
					return nil, nil
				}
				return &best, nil
			},
		}
	}

	aliceTable := &recordingTable{node: alice, eps: map[string]*netip.AddrPort{}}
	bobTable := &recordingTable{node: bob, eps: map[string]*netip.AddrPort{}}
	aliceLoop := newLoop(aliceTable, newExchanger(alicePub, alicePort),
		admission.Peer{Label: "bob", AllowedIP: netip.PrefixFrom(bobAddr, 32)},
		admission.Peer{Label: "mallory", AllowedIP: netip.PrefixFrom(malloryAddr, 32)})
	bobLoop := newLoop(bobTable, newExchanger(bobPub, bobPort),
		admission.Peer{Label: "alice", AllowedIP: netip.PrefixFrom(aliceAddr, 32)})

	// Mallory publishes an endpoint addressed to alice and waits for hers.
	malloryDone := make(chan error, 1)
	go func() {
		_, err := Exchange(relayAddr, malloryPub, alicePub, "ep1:srflx=127.0.0.1:9", 3*time.Second)
		malloryDone <- err
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); aliceLoop.SyncOnce() }()
	go func() { defer wg.Done(); bobLoop.SyncOnce() }()
	wg.Wait()

	if ep := aliceTable.eps[bobPub]; ep == nil || ep.String() != netip.AddrPortFrom(loopback, bobPort).String() {
		t.Fatalf("alice gave bob endpoint %v, want loopback:%d learned over the rendezvous", ep, bobPort)
	}
	if ep := bobTable.eps[alicePub]; ep == nil || ep.String() != netip.AddrPortFrom(loopback, alicePort).String() {
		t.Fatalf("bob gave alice endpoint %v, want loopback:%d learned over the rendezvous", ep, alicePort)
	}
	if !pingUntilConnected(t, alice, bobAddr, 8*time.Second) {
		t.Fatal("no WireGuard handshake over the rendezvous-learned endpoints")
	}

	if _, added := aliceTable.eps[malloryPub]; added {
		t.Error("alice added a peer ENS does not admit")
	}
	callsMu.Lock()
	if n := rendezvousCalls[malloryPub]; n != 0 {
		t.Errorf("alice contacted the rendezvous %d time(s) for a non-admitted peer", n)
	}
	callsMu.Unlock()
	// The Exchanger's own gate, apart from the Loop's: asked directly about
	// mallory it refuses without contacting the rendezvous.
	if _, err := aliceLoop.EndpointResolver(admission.Peer{Label: "mallory"}, malloryPub); !errors.Is(err, ErrNotAdmitted) {
		t.Errorf("EndpointResolver for a non-admitted peer returned %v, want ErrNotAdmitted", err)
	}
	callsMu.Lock()
	if n := rendezvousCalls[malloryPub]; n != 0 {
		t.Errorf("direct Learn contacted the rendezvous %d time(s) for a non-admitted peer", n)
	}
	callsMu.Unlock()
	peers, err := alice.Peers()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range peers {
		if p.PublicKeyHex == malloryPub {
			t.Error("mallory is in alice's WireGuard peer table")
		}
	}
	if len(peers) != 1 {
		t.Errorf("alice has %d peers, want only bob", len(peers))
	}

	// Mallory's offer reached the relay but nobody answered it.
	if err := <-malloryDone; err == nil {
		t.Error("mallory's exchange completed; alice should never have answered it")
	}
}
