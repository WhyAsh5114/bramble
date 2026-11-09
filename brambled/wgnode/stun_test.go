package wgnode

import (
	"context"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

// A node started before any relay existed has no STUN servers. RunSTUN must
// read the list on every pass, so servers found later are used without a
// restart.
func TestRunSTUNReadsServersOnEveryPass(t *testing.T) {
	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	node, err := New(Config{PrivateKeyHex: priv, LocalAddress: netip.MustParseAddr("10.99.0.1"), Netstack: true, EnableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()

	var passes atomic.Int32
	servers := func() []string {
		if passes.Add(1) == 1 {
			return nil
		}
		return []string{"127.0.0.1:0"} // invalid port: the pass runs and fails fast
	}
	states := make(chan EndpointStatus, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go node.runSTUN(ctx, servers, 10*time.Millisecond, func(s EndpointStatus) {
		select {
		case states <- s:
		default:
		}
	})

	first := <-states
	if first.State != "unavailable" || first.Error != "no STUN servers known yet" {
		t.Fatalf("first pass with no servers = %+v", first)
	}
	select {
	case second := <-states:
		if second.State != "unavailable" || second.Error != "STUN discovery failed" {
			t.Fatalf("second pass = %+v, want a discovery attempt against the new list", second)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunSTUN did not run a second pass")
	}
}
