package brambled

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// These tests do nothing unless deploy/nat-lab/docker-lab.sh sets
// BRAMBLE_NETLAB_ROLE. The script compiles this package's test binary and
// runs one role per network namespace, so each node's UDP leaves through its
// own NAT. They use throwaway keys the script generates for the run.

func netlabEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is not set", key)
	}
	return v
}

func TestNetlabKeygen(t *testing.T) {
	if os.Getenv("BRAMBLE_NETLAB_ROLE") != "keygen" {
		t.Skip("netlab only")
	}
	priv, pub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("NETLAB_KEY %s %s\n", priv, pub)
}

var (
	netlabDeviceIP = netip.MustParseAddr("10.99.0.1")
	netlabCloudIP  = netip.MustParseAddr("10.99.0.2")
)

func TestNetlabRole(t *testing.T) {
	role := os.Getenv("BRAMBLE_NETLAB_ROLE")
	if role != "device" && role != "cloud" {
		t.Skip("netlab only")
	}
	mode := os.Getenv("NETLAB_MODE") // "relay" (default) or "direct-control"
	priv := netlabEnv(t, "NETLAB_PRIV")
	peerPub := netlabEnv(t, "NETLAB_PEER_PUB")
	ownPub, err := rendezvous.RegisterIdentity(priv)
	if err != nil {
		t.Fatal(err)
	}

	ownIP, peerIP := netlabDeviceIP, netlabCloudIP
	if role == "cloud" {
		ownIP, peerIP = netlabCloudIP, netlabDeviceIP
	}
	listenPort := uint16(0)
	if mode == "direct-control" {
		listenPort = 51820
	}
	node, err := wgnode.New(wgnode.Config{PrivateKeyHex: priv, ListenPort: listenPort, LocalAddress: ownIP, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()

	if role == "cloud" {
		ln, err := node.ListenTCP(8080)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, "hello from the cloud node through the relay")
			}))
		}()
	}

	var endpoint *netip.AddrPort
	switch mode {
	case "direct-control":
		// The peer's NAT address with the fixed port: the best a direct
		// attempt can do without a working hole punch.
		ep := netip.MustParseAddrPort(netlabEnv(t, "NETLAB_DIRECT_ENDPOINT"))
		endpoint = &ep
	default:
		relays := staticRendezvousRelays(netlabEnv(t, "NETLAB_RELAY")) // host:port@pin
		if role == "device" {
			endpoint, _, err = relayRoutedEndpoint(node, freeSessions{}, dataRelayFlag{"relay1": relays[0]}, 10_000,
				relays, ownPub, peerPub, 40*time.Second, nil)
		} else {
			var candidate string
			candidate, _, err = exchangeAny(relays, ownPub, peerPub, "", 40*time.Second)
			if err == nil {
				var addr netip.AddrPort
				if addr, err = peerEndpointFromCandidate(candidate); err == nil {
					endpoint = &addr
					adoptBind(node, candidate, addr)
				}
			}
		}
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
	}
	fmt.Printf("NETLAB %s endpoint=%s\n", role, endpoint)
	if err := node.AddPeer(peerPub, netip.PrefixFrom(peerIP, 32), endpoint); err != nil {
		t.Fatal(err)
	}
	// docker-lab.sh waits for this line before it trusts a negative control.
	fmt.Printf("NETLAB_READY %s\n", role)

	if role == "cloud" {
		time.Sleep(90 * time.Second) // the script kills this process when the device is done
		return
	}

	if mode == "direct-control" {
		for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
			if ok, _ := node.Ping(peerIP, time.Second); ok {
				t.Fatal("direct path worked; the lab is not defeating hole punching")
			}
		}
		fmt.Println("NETLAB_RESULT direct-control: no handshake in 12s (direct path fails)")
		return
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return node.DialTCP(netip.AddrPortFrom(peerIP, 8080))
		}},
	}
	var resp *http.Response
	for deadline := time.Now().Add(40 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		if resp, err = client.Get("http://cloud.mesh/"); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("HTTP GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("NETLAB_RESULT relay: HTTP %d %q\n", resp.StatusCode, strings.TrimSpace(string(body)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
