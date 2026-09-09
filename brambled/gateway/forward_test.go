package gateway

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// TestForwarder_PlainClientProxiedThroughACL is the property the whole
// package exists to provide: an ordinary net.Dial client — no CONNECT
// preamble, no knowledge that a gateway or an ACL exists — connects to a
// local port and transparently reaches an authorized service on a real
// WireGuard tunnel, with a real local backend answering on the other end.
func TestForwarder_PlainClientProxiedThroughACL(t *testing.T) {
	devicePriv, devicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	gwPriv, gwPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	granterPriv := strings.Repeat("55", 32)
	granterPub, err := publicFromPrivate(granterPriv)
	if err != nil {
		t.Fatal(err)
	}

	deviceAddr := netip.MustParseAddr("10.102.0.1")
	gwAddr := netip.MustParseAddr("10.102.0.2")

	device, err := wgnode.New(wgnode.Config{PrivateKeyHex: devicePriv, ListenPort: 62960, LocalAddress: deviceAddr, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()

	gw, err := wgnode.New(wgnode.Config{PrivateKeyHex: gwPriv, ListenPort: 62961, LocalAddress: gwAddr, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	gwEp := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 62961)
	if err := device.AddPeer(gwPub, netip.PrefixFrom(gwAddr, 32), &gwEp); err != nil {
		t.Fatal(err)
	}
	deviceEp := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 62960)
	if err := gw.AddPeer(devicePub, netip.PrefixFrom(deviceAddr, 32), &deviceEp); err != nil {
		t.Fatal(err)
	}
	if !pingUntilConnected(t, device, gwAddr, 8*time.Second) {
		t.Fatal("no handshake")
	}

	digest, err := ACLDigestECDH(granterPriv, gwPub, "web")
	if err != nil {
		t.Fatal(err)
	}
	resolver := fakeResolver{
		"device1":  {ACL: []string{digest}},
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: &granterPub},
	}

	const banner = "hello from the real backend, reached with zero CONNECT knowledge"
	backendPort := startLocalBackend(t, banner)

	gwLn, err := gw.ListenTCP(7300)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		Listener:      gwLn,
		Resolver:      resolver,
		PrivateKeyHex: gwPriv,
		OwnLabel:      "gateway1",
		Services:      map[string]uint16{"web": backendPort},
		PeerLabels:    map[netip.Addr]string{deviceAddr: "device1"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)

	// The Forwarder's local listener is a REAL OS socket (net.Listen, not
	// netstack) — this is the whole point: an unmodified external client
	// must be able to reach it with a plain net.Dial, same as it would any
	// other local port. The tunnel-side dial is the only netstack-specific
	// part.
	localLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer localLn.Close()
	localPort := localLn.Addr().(*net.TCPAddr).Port

	fwd := &Forwarder{
		Listener: localLn,
		DialTunnel: func() (net.Conn, error) {
			return device.DialTCP(netip.AddrPortFrom(gwAddr, 7300))
		},
		Service: "web",
	}
	go fwd.Serve(ctx)

	t.Run("plain client reaches the backend with zero CONNECT knowledge", func(t *testing.T) {
		conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(localPort))
		if err != nil {
			t.Fatalf("dialing local forward port: %v", err)
		}
		defer conn.Close()

		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		got, err := io.ReadAll(conn)
		if err != nil && err != io.EOF {
			t.Fatalf("reading proxied response: %v", err)
		}
		if string(got) != banner {
			t.Fatalf("expected the real backend's banner, got %q", string(got))
		}
	})

	t.Run("denial counterpart: no ACL for this service closes the local connection", func(t *testing.T) {
		noACLResolver := fakeResolver{
			"device1":  {ACL: nil}, // no digest at all — never granted
			"gateway1": {ACLGranters: []string{"granter1"}},
			"granter1": {Pubkey: &granterPub},
		}
		gwLn2, err := gw.ListenTCP(7301)
		if err != nil {
			t.Fatal(err)
		}
		srv2 := &Server{
			Listener:      gwLn2,
			Resolver:      noACLResolver,
			PrivateKeyHex: gwPriv,
			OwnLabel:      "gateway1",
			Services:      map[string]uint16{"web": backendPort},
			PeerLabels:    map[netip.Addr]string{deviceAddr: "device1"},
		}
		go srv2.Serve(ctx)

		localLn2, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer localLn2.Close()
		localPort2 := localLn2.Addr().(*net.TCPAddr).Port

		fwd2 := &Forwarder{
			Listener: localLn2,
			DialTunnel: func() (net.Conn, error) {
				return device.DialTCP(netip.AddrPortFrom(gwAddr, 7301))
			},
			Service: "web",
		}
		go fwd2.Serve(ctx)
		time.Sleep(100 * time.Millisecond)

		conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(localPort2))
		if err != nil {
			t.Fatalf("dialing local forward port: %v", err)
		}
		defer conn.Close()

		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		got, err := io.ReadAll(conn)
		if err != nil && err != io.EOF {
			t.Fatalf("reading proxied response: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected the local connection to be closed with no data (denied), got %q", string(got))
		}
	})
}
