package gateway

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

func pingUntilConnected(t *testing.T, from *wgnode.Node, to netip.Addr, deadline time.Duration) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		ok, err := from.Ping(to, 1*time.Second)
		if err != nil {
			t.Fatalf("ping errored: %v", err)
		}
		if ok {
			return true
		}
	}
	return false
}

// startLocalBackend starts a plain TCP listener on 127.0.0.1 that writes
// banner to every connection it accepts, standing in for the "real" local
// service a gateway forwards an authorized CONNECT to. Returns its port.
func startLocalBackend(t *testing.T, banner string) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting local backend: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				fmt.Fprint(conn, banner)
			}()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return uint16(addr.Port)
}

// sendConnect writes "CONNECT <service>\n" over conn and returns the first
// response line ("OK" or "DENY") plus anything the backend sent afterward
// (only meaningful on "OK").
func sendConnect(t *testing.T, conn net.Conn, service string) (status, payload string) {
	t.Helper()
	if _, err := fmt.Fprintf(conn, "CONNECT %s\n", service); err != nil {
		t.Fatalf("writing CONNECT: %v", err)
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	status = strings.TrimSpace(line)
	if status != "OK" {
		return status, ""
	}
	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _ := reader.Read(buf)
	return status, string(buf[:n])
}

// TestGate2_3_AgentCannotReachOutsideACL is Gate 2.3's required test
// (docs/05_BUILD_PLAN.md line 58-59): a device with a `db`-only grant sends
// CONNECT cache on the same host, and CONNECT db on a different host
// entirely — both refused by the receiving peer — while CONNECT db on the
// authorized host succeeds and actually proxies real bytes end to end over
// a real WireGuard tunnel.
func TestGate2_3_AgentCannotReachOutsideACL(t *testing.T) {
	devicePriv, devicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating device key pair: %v", err)
	}
	gwAPriv, gwAPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating gatewayA key pair: %v", err)
	}
	gwBPriv, gwBPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generating gatewayB key pair: %v", err)
	}
	granterPriv := strings.Repeat("ee", 32)
	granterPub, err := publicFromPrivate(granterPriv)
	if err != nil {
		t.Fatal(err)
	}

	deviceAddr := netip.MustParseAddr("10.100.0.1")
	gwAAddr := netip.MustParseAddr("10.100.0.2")
	gwBAddr := netip.MustParseAddr("10.100.0.3")

	device, err := wgnode.New(wgnode.Config{PrivateKeyHex: devicePriv, ListenPort: 62950, LocalAddress: deviceAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting device: %v", err)
	}
	defer device.Close()

	gwA, err := wgnode.New(wgnode.Config{PrivateKeyHex: gwAPriv, ListenPort: 62951, LocalAddress: gwAAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting gatewayA: %v", err)
	}
	defer gwA.Close()

	gwB, err := wgnode.New(wgnode.Config{PrivateKeyHex: gwBPriv, ListenPort: 62952, LocalAddress: gwBAddr, Netstack: true})
	if err != nil {
		t.Fatalf("starting gatewayB: %v", err)
	}
	defer gwB.Close()

	// Direct static peering both directions, no rendezvous relay involved —
	// Section C is about the CONNECT protocol and ACL check, not signaling.
	mustAddPeer := func(from *wgnode.Node, pub string, addr netip.Addr, port uint16) {
		t.Helper()
		ep := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)
		if err := from.AddPeer(pub, netip.PrefixFrom(addr, 32), &ep); err != nil {
			t.Fatalf("AddPeer: %v", err)
		}
	}
	mustAddPeer(device, gwAPub, gwAAddr, 62951)
	mustAddPeer(device, gwBPub, gwBAddr, 62952)
	mustAddPeer(gwA, devicePub, deviceAddr, 62950)
	mustAddPeer(gwB, devicePub, deviceAddr, 62950)

	if !pingUntilConnected(t, device, gwAAddr, 8*time.Second) {
		t.Fatal("device never completed a handshake with gatewayA")
	}
	if !pingUntilConnected(t, device, gwBAddr, 8*time.Second) {
		t.Fatal("device never completed a handshake with gatewayB")
	}

	// device's granted digest is computed specifically against gatewayA's
	// pubkey — this is the on-chain state Section B's set-acl.ts would have
	// written. gatewayB is a real, separately-keyed gateway that also
	// trusts granter1 (isolating the "digests aren't portable across
	// gateways" property, not merely "gatewayB doesn't trust this granter").
	digestForA, err := ACLDigestECDH(granterPriv, gwAPub, devicePub, "db")
	if err != nil {
		t.Fatal(err)
	}

	future := time.Now().Add(time.Hour).Unix()
	resolver := fakeResolver{
		"device1":  {Pubkey: &devicePub, Expiry: fmt.Sprint(future), ACL: []string{digestForA}},
		"gatewayA": {ACLGranters: []string{"granter1"}},
		"gatewayB": {ACLGranters: []string{"granter1"}},
		"granter1": {Pubkey: &granterPub},
	}

	dbBackendPortA := startLocalBackend(t, "db-backend-on-A")
	cacheBackendPortA := startLocalBackend(t, "cache-backend-on-A")
	dbBackendPortB := startLocalBackend(t, "db-backend-on-B")

	peerLabels := map[netip.Addr]string{deviceAddr: "device1"}

	serverA := &Server{
		Resolver:      resolver,
		PrivateKeyHex: gwAPriv,
		OwnLabel:      "gatewayA",
		Services:      map[string]uint16{"db": dbBackendPortA, "cache": cacheBackendPortA},
		PeerLabels:    peerLabels,
	}
	serverB := &Server{
		Resolver:      resolver,
		PrivateKeyHex: gwBPriv,
		OwnLabel:      "gatewayB",
		Services:      map[string]uint16{"db": dbBackendPortB},
		PeerLabels:    peerLabels,
	}

	lnA, err := gwA.ListenTCP(7100)
	if err != nil {
		t.Fatalf("gatewayA listening: %v", err)
	}
	serverA.Listener = lnA
	lnB, err := gwB.ListenTCP(7100)
	if err != nil {
		t.Fatalf("gatewayB listening: %v", err)
	}
	serverB.Listener = lnB

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serverA.Serve(ctx)
	go serverB.Serve(ctx)

	// Positive control: CONNECT db on the authorized gateway succeeds and
	// really proxies bytes from the local backend, over the real tunnel.
	t.Run("authorized service on authorized gateway succeeds", func(t *testing.T) {
		conn, err := device.DialTCP(netip.AddrPortFrom(gwAAddr, 7100))
		if err != nil {
			t.Fatalf("dialing gatewayA: %v", err)
		}
		defer conn.Close()
		status, payload := sendConnect(t, conn, "db")
		if status != "OK" {
			t.Fatalf("expected OK, got %q", status)
		}
		if payload != "db-backend-on-A" {
			t.Fatalf("expected proxied backend banner, got %q", payload)
		}
	})

	// Gate 2.3, scenario 1: CONNECT cache on the same (authorized-for-db)
	// host is refused.
	t.Run("wrong service on same host denied", func(t *testing.T) {
		conn, err := device.DialTCP(netip.AddrPortFrom(gwAAddr, 7100))
		if err != nil {
			t.Fatalf("dialing gatewayA: %v", err)
		}
		defer conn.Close()
		status, _ := sendConnect(t, conn, "cache")
		if status != "DENY" {
			t.Fatalf("expected DENY for cache on gatewayA, got %q", status)
		}
	})

	// Gate 2.3, scenario 2: CONNECT db on a different host entirely is
	// refused, even though gatewayB also trusts granter1 — the digest
	// device1 holds was computed for gatewayA's pubkey specifically.
	t.Run("right service on different host denied", func(t *testing.T) {
		conn, err := device.DialTCP(netip.AddrPortFrom(gwBAddr, 7100))
		if err != nil {
			t.Fatalf("dialing gatewayB: %v", err)
		}
		defer conn.Close()
		status, _ := sendConnect(t, conn, "db")
		if status != "DENY" {
			t.Fatalf("expected DENY for db on gatewayB, got %q", status)
		}
	})

	// Gate 5.1's integrated permission-escalation flow: the same request
	// denied above succeeds immediately after the resolver reports a new
	// capability, without restarting either node or gateway.
	t.Run("same request succeeds after live capability expansion", func(t *testing.T) {
		cacheDigest, err := ACLDigestECDH(granterPriv, gwAPub, devicePub, "cache")
		if err != nil {
			t.Fatal(err)
		}
		resolver["device1"].ACL = append(resolver["device1"].ACL, cacheDigest)

		conn, err := device.DialTCP(netip.AddrPortFrom(gwAAddr, 7100))
		if err != nil {
			t.Fatalf("dialing gatewayA: %v", err)
		}
		defer conn.Close()
		status, payload := sendConnect(t, conn, "cache")
		if status != "OK" || payload != "cache-backend-on-A" {
			t.Fatalf("expected expanded grant to proxy the cache task, got status=%q payload=%q", status, payload)
		}
	})

	// Finish the task lifecycle by applying the same admission loop used by
	// brambled serve. A fresh revoked record removes the already-connected
	// peer, and the tunnel can no longer carry even the previously allowed
	// task.
	t.Run("device revocation drops the task connection", func(t *testing.T) {
		loop := &admission.Loop{
			Resolver: resolver,
			Table:    gwA,
			Peers:    []admission.Peer{{Label: "device1", AllowedIP: netip.PrefixFrom(deviceAddr, 32)}},
		}
		loop.SyncOnce()
		resolver["device1"].Revoked = true
		loop.SyncOnce()

		peers, err := gwA.Peers()
		if err != nil {
			t.Fatal(err)
		}
		for _, peer := range peers {
			if peer.PublicKeyHex == devicePub {
				t.Fatal("revoked device remained in gatewayA's WireGuard peer table")
			}
		}
		ok, err := device.Ping(gwAAddr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("revoked device could still complete a task over the tunnel")
		}
	})
}
