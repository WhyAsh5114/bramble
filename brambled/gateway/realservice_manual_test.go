//go:build manual

// This file is excluded from normal `go test ./...` (see the build tag
// above) because it depends on live external processes: a real Postgres
// instance and a real Node HTTP server, both started by hand before running
// `go test -tags manual ./gateway/... -run TestManual`. It exists to answer
// a direct question, empirically, not by argument: the gateway's proxy
// (server.go's `proxy` function) is a raw bidirectional io.Copy — it never
// parses the bytes flowing through it — so nothing about it is Go-specific
// or protocol-specific on the backend side. This test proves that against
// two real, independently-implemented backends rather than the synthetic
// banner-writer server_test.go uses for its faster, hermetic CI runs.
package gateway

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// TestManual_RealPostgresAndHTTPBackendsProxyCorrectly requires, running
// before this test:
//
//	docker run -d --name gw-verify-postgres -p 55490:5432 -e POSTGRES_PASSWORD=test postgres:16-alpine
//	node -e 'require("http").createServer((q,r)=>{r.writeHead(200);r.end("hello from a real node http server, path="+q.url)}).listen(45990,"127.0.0.1")'
//
// Run with: go test -tags manual ./gateway/... -run TestManual_RealPostgresAndHTTPBackendsProxyCorrectly -v
func TestManual_RealPostgresAndHTTPBackendsProxyCorrectly(t *testing.T) {
	devicePriv, devicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	gwPriv, gwPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	granterPriv := strings.Repeat("77", 32)
	granterPub, err := publicFromPrivate(granterPriv)
	if err != nil {
		t.Fatal(err)
	}

	deviceAddr := netip.MustParseAddr("10.101.0.1")
	gwAddr := netip.MustParseAddr("10.101.0.2")

	device, err := wgnode.New(wgnode.Config{PrivateKeyHex: devicePriv, ListenPort: 62970, LocalAddress: deviceAddr, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()

	gw, err := wgnode.New(wgnode.Config{PrivateKeyHex: gwPriv, ListenPort: 62971, LocalAddress: gwAddr, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	ep := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 62971)
	if err := device.AddPeer(gwPub, netip.PrefixFrom(gwAddr, 32), &ep); err != nil {
		t.Fatal(err)
	}
	epDev := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 62970)
	if err := gw.AddPeer(devicePub, netip.PrefixFrom(deviceAddr, 32), &epDev); err != nil {
		t.Fatal(err)
	}
	if !pingUntilConnected(t, device, gwAddr, 8*time.Second) {
		t.Fatal("no handshake")
	}

	dbDigest, err := ACLDigestECDH(granterPriv, gwPub, "db")
	if err != nil {
		t.Fatal(err)
	}
	webDigest, err := ACLDigestECDH(granterPriv, gwPub, "web")
	if err != nil {
		t.Fatal(err)
	}

	resolver := fakeResolver{
		"device1":  {ACL: []string{dbDigest, webDigest}},
		"gateway1": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: &granterPub},
	}

	ln, err := gw.ListenTCP(7200)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		Listener:      ln,
		Resolver:      resolver,
		PrivateKeyHex: gwPriv,
		OwnLabel:      "gateway1",
		Services:      map[string]uint16{"db": 55490, "web": 45990},
		PeerLabels:    map[netip.Addr]string{deviceAddr: "device1"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)

	t.Run("real postgres backend", func(t *testing.T) {
		conn, err := device.DialTCP(netip.AddrPortFrom(gwAddr, 7200))
		if err != nil {
			t.Fatalf("dialing gateway: %v", err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT db\n")
		status, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(status) != "OK" {
			t.Fatalf("expected OK, got %q", status)
		}

		// A real Postgres StartupMessage, hand-encoded (protocol 3.0, user=postgres).
		params := "user\x00postgres\x00database\x00postgres\x00\x00"
		msg := make([]byte, 0, 8+len(params))
		length := 4 + 4 + len(params)
		msg = append(msg, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
		msg = append(msg, 0x00, 0x03, 0x00, 0x00) // protocol version 3.0
		msg = append(msg, params...)
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("writing postgres startup message: %v", err)
		}

		resp := make([]byte, 1)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Read(resp); err != nil {
			t.Fatalf("reading postgres response through the proxy: %v", err)
		}
		// 'R' = AuthenticationXXX, a real postgres backend engaging with our
		// startup packet. Anything else means it's not really talking postgres.
		if resp[0] != 'R' {
			t.Fatalf("expected postgres AuthenticationRequest ('R'), got %q — not a real postgres response", resp[0])
		}
		t.Log("real Postgres backend responded with a genuine AuthenticationRequest through the gateway proxy")
	})

	t.Run("real node http backend", func(t *testing.T) {
		conn, err := device.DialTCP(netip.AddrPortFrom(gwAddr, 7200))
		if err != nil {
			t.Fatalf("dialing gateway: %v", err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT web\n")
		reader := bufio.NewReader(conn)
		status, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(status) != "OK" {
			t.Fatalf("expected OK, got %q", status)
		}

		fmt.Fprintf(conn, "GET /proxied-through-gateway HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatalf("reading HTTP response through the proxy: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		buf := make([]byte, 256)
		n, _ := resp.Body.Read(buf)
		body := string(buf[:n])
		if !strings.Contains(body, "proxied-through-gateway") {
			t.Fatalf("expected the real node server's echoed path in the body, got %q", body)
		}
		t.Logf("real Node http.Server responded through the gateway proxy: %q", body)
	})

	t.Run("no ACL for this service is denied, real backend never contacted", func(t *testing.T) {
		conn, err := device.DialTCP(netip.AddrPortFrom(gwAddr, 7200))
		if err != nil {
			t.Fatalf("dialing gateway: %v", err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT admin\n") // no digest for "admin" exists anywhere
		status, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(status) != "DENY" {
			t.Fatalf("expected DENY for a service with no granted ACL, got %q", status)
		}
	})

	t.Run("counterpart: granted service denied once its digest is removed from the device's acl", func(t *testing.T) {
		// Same real postgres backend, same gateway trust — but this time the
		// device's own acl record (fakeResolver's "device1") doesn't carry
		// the digest for "db" at all, simulating a device that was never
		// granted this capability on-chain.
		noACLResolver := fakeResolver{
			"device1":  {ACL: nil},
			"gateway1": {ACLGranters: []string{"granter1"}},
			"granter1": {Pubkey: &granterPub},
		}
		srv2 := &Server{
			Resolver:      noACLResolver,
			PrivateKeyHex: gwPriv,
			OwnLabel:      "gateway1",
			Services:      map[string]uint16{"db": 55490},
			PeerLabels:    map[netip.Addr]string{deviceAddr: "device1"},
		}
		ln2, err := gw.ListenTCP(7201)
		if err != nil {
			t.Fatal(err)
		}
		srv2.Listener = ln2
		go srv2.Serve(ctx)
		time.Sleep(100 * time.Millisecond)

		conn, err := device.DialTCP(netip.AddrPortFrom(gwAddr, 7201))
		if err != nil {
			t.Fatalf("dialing gateway: %v", err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT db\n")
		status, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(status) != "DENY" {
			t.Fatalf("expected DENY when device1's acl has no db digest, got %q", status)
		}
	})
}
