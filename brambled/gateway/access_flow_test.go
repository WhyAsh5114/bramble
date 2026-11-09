package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/internal/testport"
	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// The Bun e2e test supplies in-memory ENS records over loopback HTTP. Neither
// approval status nor a test-controlled allow/deny flag enters the gateway.
type accessFlowResolver struct{ url string }

func (r accessFlowResolver) ResolveDevice(label string) (*registry.DeviceRecord, error) {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(r.url + "/devices/" + label)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("in-memory resolver: HTTP %d", resp.StatusCode)
	}
	var record registry.DeviceRecord
	err = json.NewDecoder(resp.Body).Decode(&record)
	return &record, err
}

// TestAccessFlowFixture is a test-only subprocess, launched by agent-mcp's
// access-flow.test.ts. Ordinary Go tests skip it; it has no production entrypoint.
func TestAccessFlowFixture(t *testing.T) {
	resolverURL := os.Getenv("BRAMBLE_ACCESS_FLOW_RESOLVER_URL")
	if resolverURL == "" {
		t.Skip("launched by pnpm test:access-flow")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	newNode := func(addr string) (*wgnode.Node, string, string, uint16) {
		t.Helper()
		priv, pub, err := wgnode.GenerateKeyPair()
		if err != nil {
			t.Fatal("generating fixture identity")
		}
		port := testport.FreeUDP(t)
		node, err := wgnode.New(wgnode.Config{
			PrivateKeyHex: priv, ListenPort: port,
			LocalAddress: netip.MustParseAddr(addr), Netstack: true,
		})
		if err != nil {
			t.Fatal("starting userspace WireGuard fixture")
		}
		t.Cleanup(func() { node.Close() })
		return node, priv, pub, port
	}
	agentAddr := netip.MustParseAddr("10.100.33.1")
	gatewayAddr := netip.MustParseAddr("10.100.33.2")
	agent, _, agentPub, agentPort := newNode(agentAddr.String())
	gw, gatewayPriv, gatewayPub, gatewayPort := newNode(gatewayAddr.String())
	for _, peer := range []struct {
		node *wgnode.Node
		pub  string
		addr netip.Addr
		port uint16
	}{{agent, gatewayPub, gatewayAddr, gatewayPort}, {gw, agentPub, agentAddr, agentPort}} {
		ep := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), peer.port)
		if err := peer.node.AddPeer(peer.pub, netip.PrefixFrom(peer.addr, 32), &ep); err != nil {
			t.Fatal("adding fixture peer")
		}
	}
	if !pingUntilConnected(t, agent, gatewayAddr, 8*time.Second) {
		t.Fatal("fixture WireGuard handshake failed")
	}

	var mu sync.Mutex
	decisions := []Decision{}
	backendPaths := []string{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		backendPaths = append(backendPaths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Connection", "close")
		fmt.Fprint(w, "private task over WireGuard")
	}))
	defer backend.Close()
	backendPort := uint16(backend.Listener.Addr().(*net.TCPAddr).Port)
	listener, err := gw.ListenTCP(7100)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		Listener: listener, Resolver: accessFlowResolver{resolverURL},
		PrivateKeyHex: gatewayPriv, OwnLabel: "gateway-1",
		Services:   map[string]uint16{"staging-api": backendPort},
		PeerLabels: map[netip.Addr]string{agentAddr: "agent-1"},
		Logf:       func(string, ...any) {},
		OnDecision: func(d Decision) {
			mu.Lock()
			defer mu.Unlock()
			decisions = append(decisions, d)
		},
	}
	go server.Serve(ctx)
	local, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	forward := &Forwarder{
		Listener: local, Service: "staging-api", Logf: func(string, ...any) {},
		DialTunnel: func() (net.Conn, error) {
			return agent.DialTCP(netip.AddrPortFrom(gatewayAddr, 7100))
		},
	}
	go forward.Serve(ctx)

	stop := make(chan struct{})
	var once sync.Once
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/stop" {
			once.Do(func() { close(stop) })
			return
		}
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"decisions": decisions, "backendPaths": backendPaths})
	}))
	defer control.Close()
	ready, _ := json.Marshal(map[string]string{
		"forwardUrl": "http://" + local.Addr().String(), "controlUrl": control.URL,
		"agentPub": agentPub, "gatewayPub": gatewayPub,
	})
	fmt.Printf("ACCESS_FLOW_READY %s\n", ready)
	select {
	case <-stop:
	case <-time.After(40 * time.Second):
		t.Fatal("access-flow driver did not stop fixture")
	}
}
