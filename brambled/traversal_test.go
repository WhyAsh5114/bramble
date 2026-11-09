package brambled

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

func TestAdmissionTraversalSharesOneRelayAndStopsOnRevoke(t *testing.T) {
	relay, _ := startRelay(t, "test")
	payer := &countingSessions{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var nodes [2]*wgnode.Node
	var keys [2]string
	addresses := [2]netip.Addr{netip.MustParseAddr("10.88.0.1"), netip.MustParseAddr("10.88.0.2")}
	for i := range nodes {
		private, public, err := wgnode.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rendezvous.RegisterIdentity(private); err != nil {
			t.Fatal(err)
		}
		keys[i] = public
		nodes[i], err = wgnode.New(wgnode.Config{PrivateKeyHex: private, Netstack: true, EnableSTUN: true, LocalAddress: addresses[i]})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nodes[i].Close)
	}
	managers := [2]*peerTraversal{}
	for i := range nodes {
		peer := 1 - i
		if err := nodes[i].AddPeer(keys[peer], netip.PrefixFrom(addresses[peer], 32), nil); err != nil {
			t.Fatal(err)
		}
		managers[i] = &peerTraversal{ctx: ctx, node: nodes[i], own: keys[i], sessions: payer, pool: dataRelayFlag{"test": relay}, sessionBytes: 10000, exchangeTimeout: time.Second, allowLoopback: true,
			local:    func() rendezvous.Announcement { return nil }, // no manual peer endpoint
			relaySet: func() ([]rendezvous.Relay, error) { return []rendezvous.Relay{relay}, nil },
		}
		event := admission.Event{Label: "other", PublicKey: keys[peer], Authorized: true}
		managers[i].Handle(event)
		managers[i].Handle(event) // repeated ENS polls must not start duplicate buyers
	}
	t.Cleanup(func() {
		cancel()
		for _, m := range managers {
			for _, run := range m.runs {
				select {
				case <-run.done:
				case <-time.After(3 * time.Second):
					t.Error("traversal did not stop")
				}
			}
		}
	})
	deadline := time.Now().Add(25 * time.Second)
	connected := false
	for time.Now().Before(deadline) {
		if ok, err := nodes[0].Ping(addresses[1], 200*time.Millisecond); err == nil && ok {
			connected = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !connected {
		t.Fatal("automatic shared-relay tunnel did not connect")
	}
	if n := payer.n.Load(); n != 1 {
		t.Fatalf("allocated %d sessions; need one elected buyer", n)
	}
	a, _ := nodes[0].Peers()
	b, _ := nodes[1].Peers()
	if len(a) != 1 || len(b) != 1 || a[0].Endpoint != b[0].Endpoint {
		t.Fatalf("different relay paths: %v %v", a, b)
	}
	run := managers[0].runs["other"]
	if err := nodes[0].RemovePeer(keys[1]); err != nil {
		t.Fatal(err)
	}
	managers[0].Handle(admission.Event{Label: "other", PublicKey: keys[1], Authorized: false})
	select {
	case <-run.done:
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not stop traversal")
	}
	peers, _ := nodes[0].Peers()
	if len(peers) != 0 {
		t.Fatal("traversal recreated revoked peer")
	}
}

func TestForcedRelayIsReportedAsRelayedByAutomaticPeer(t *testing.T) {
	relay, _ := startRelay(t, "test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var nodes [2]*wgnode.Node
	var keys [2]string
	addresses := [2]netip.Addr{netip.MustParseAddr("10.89.0.1"), netip.MustParseAddr("10.89.0.2")}
	for i := range nodes {
		private, public, err := wgnode.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rendezvous.RegisterIdentity(private); err != nil {
			t.Fatal(err)
		}
		keys[i] = public
		nodes[i], err = wgnode.New(wgnode.Config{PrivateKeyHex: private, Netstack: true, EnableSTUN: true, LocalAddress: addresses[i]})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nodes[i].Close)
		if i == 1 {
			if err := nodes[1].AddPeer(keys[0], netip.PrefixFrom(addresses[0], 32), nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	relays := []rendezvous.Relay{relay}
	forcedDone := make(chan error, 1)
	go func() {
		ep, _, err := relayRoutedEndpoint(nodes[0], freeSessions{}, dataRelayFlag{"test": relay}, 10000, relays, keys[0], keys[1], 3*time.Second, nil)
		if err == nil {
			err = nodes[0].AddPeer(keys[1], netip.PrefixFrom(addresses[1], 32), ep)
		}
		forcedDone <- err
	}()
	states := make(chan string, 100)
	manager := &peerTraversal{ctx: ctx, node: nodes[1], own: keys[1], sessions: freeSessions{}, allowLoopback: true, exchangeTimeout: 3 * time.Second,
		local: func() rendezvous.Announcement { return nil }, relaySet: func() ([]rendezvous.Relay, error) { return relays, nil },
		onState: func(_, _, state string) { states <- state },
	}
	manager.Handle(admission.Event{Label: "forced", PublicKey: keys[0], Authorized: true})
	t.Cleanup(func() {
		cancel()
		select {
		case <-manager.runs["forced"].done:
		case <-time.After(time.Second):
			t.Error("automatic traversal did not stop")
		}
	})
	if err := <-forcedDone; err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case state := <-states:
			if state == "direct" {
				t.Fatal("relay session was falsely reported as direct")
			}
			if state == "relay" {
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					if ok, err := nodes[1].Ping(addresses[0], 300*time.Millisecond); err == nil && ok {
						return
					}
					time.Sleep(50 * time.Millisecond)
				}
				t.Fatal("mixed forced/automatic relay tunnel did not pass traffic")
			}
		case <-timer.C:
			t.Fatal("automatic peer did not adopt forced relay")
		}
	}
}
