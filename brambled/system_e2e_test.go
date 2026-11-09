package brambled

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/internal/testport"
	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/registry/registrytest"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

type e2eNode struct {
	label, priv, pub string
	handle           chan *Handle
	tn               *registry.Tailnet
}

// TestSystemTwoNodesThroughTheRelay runs the whole path the product uses:
// chain events -> registry cache -> admission -> sealed rendezvous -> bound
// data-relay session -> gateway LIST/CONNECT -> revocation. Run it with
// BRAMBLE_TRACE=1 RELAY_TRACE=1 -v to see each step.
func TestSystemTwoNodesThroughTheRelay(t *testing.T) {
	chain := registrytest.New(t, "e2e.net.bramble.eth")
	relay, _ := startRelay(t, "r1")
	relayFlag := relay.Address + "@" + relay.Pin

	nodes := map[string]*e2eNode{}
	for i, label := range []string{"laptop", "server"} {
		priv, pub, err := wgnode.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		nodes[label] = &e2eNode{label: label, priv: priv, pub: pub, handle: make(chan *Handle, 1)}
		chain.Enroll(label, map[string]string{
			registry.KeyPubkey: pub,
			registry.KeyMeshIP: fmt.Sprintf("10.130.0.%d/24", i+1),
		})
	}

	// The server's open "ping" service.
	pingLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pingLn.Close()
	go func() {
		for {
			c, err := pingLn.Accept()
			if err != nil {
				return
			}
			fmt.Fprint(c, "pong from server")
			c.Close()
		}
	}()
	pingPort := pingLn.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	for _, n := range nodes {
		tn, err := registry.NewTailnet(chain.Config(""))
		if err != nil {
			t.Fatal(err)
		}
		n.tn = tn
		if err := tn.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		go tn.Run(ctx)
		args := []string{
			"-label", n.label, "-private-key", n.priv, "-netstack", "-status-port", "0",
			"-listen-port", strconv.Itoa(int(testport.FreeUDP(t))), "-ttl", "2s",
			"-rendezvous", relayFlag, "-data-relay", "r1=" + relayFlag, "-relay-session-bytes", "1000000",
		}
		if n.label == "laptop" {
			args = append(args, "-relay-first") // no direct path: everything goes through the relay
		} else {
			args = append(args, "-service", fmt.Sprintf("ping=%d/open", pingPort), "-service", fmt.Sprintf("secret=%d", pingPort))
		}
		n := n
		go func() { done <- Serve(ctx, tn, args, func(h *Handle) { n.handle <- h }) }()
	}
	laptop := <-nodes["laptop"].handle
	<-nodes["server"].handle

	start := time.Now()
	var services []string
	for time.Since(start) < 40*time.Second {
		if list, err := laptop.Services("server"); err == nil {
			for _, s := range list {
				services = append(services, s.Name+"/"+s.Access)
			}
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(services) != 1 || services[0] != "ping/tailnet" {
		t.Fatalf("laptop sees services %v; want only the open ping service", services)
	}
	t.Logf("laptop listed server's services after %s", time.Since(start))

	conn, err := laptop.Dial("server", "ping")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(conn)
	conn.Close()
	if string(body) != "pong from server" {
		t.Fatalf("ping answered %q", body)
	}
	if _, err := laptop.Dial("server", "secret"); err == nil {
		t.Fatal("an ungranted service was reachable")
	}
	peers, _ := laptop.Peers()
	for _, p := range peers {
		t.Logf("laptop sees %s: authorized=%v path=%s endpoint=%s", p.Label, p.Authorized, p.Path, p.Endpoint)
	}

	// Revoke the laptop on chain: the server stops admitting it.
	chain.SetText("laptop", registry.KeyRevoked, "true")
	revokedAt := time.Now()
	for time.Since(revokedAt) < 30*time.Second {
		c, err := laptop.Dial("server", "ping")
		if err != nil {
			t.Logf("server cut the laptop off %s after the revocation block", time.Since(revokedAt).Round(100*time.Millisecond))
			cancel()
			return
		}
		c.Close()
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("revoked laptop still reaches the server")
}
