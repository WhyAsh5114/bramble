package statusapi_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/statusapi"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

func TestSTUNEndpointRootless(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	observed := make(chan netip.AddrPort, 1)
	go func() {
		req := make([]byte, 2048)
		n, src, err := server.ReadFromUDPAddrPort(req)
		if err != nil || n != 20 {
			return
		}
		reply := make([]byte, 32)
		copy(reply, req[:20])
		binary.BigEndian.PutUint16(reply, 0x101)
		binary.BigEndian.PutUint16(reply[2:], 12)
		binary.BigEndian.PutUint16(reply[20:], 0x20)
		binary.BigEndian.PutUint16(reply[22:], 8)
		reply[25] = 1
		binary.BigEndian.PutUint16(reply[26:], src.Port()^0x2112)
		for i, v := range src.Addr().AsSlice() {
			reply[28+i] = v ^ req[4+i]
		}
		server.WriteToUDPAddrPort(reply, src)
		observed <- src
	}()
	priv, pub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	node, err := wgnode.New(wgnode.Config{PrivateKeyHex: priv, EnableSTUN: true, Netstack: true, LocalAddress: netip.MustParseAddr("10.77.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	api := &statusapi.Server{PeerTable: node, EndpointSource: node}
	check := func(want string) wgnode.EndpointStatus {
		t.Helper()
		rec := httptest.NewRecorder()
		api.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/endpoint", nil))
		var state wgnode.EndpointStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || state.State != want {
			t.Fatalf("status: %d %+v", rec.Code, state)
		}
		return state
	}
	check("disabled")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	endpoint, err := node.DiscoverEndpoint(ctx, []string{server.LocalAddr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != <-observed {
		t.Fatal("mapping differs from observed socket")
	}
	state := check("discovered")
	if state.Endpoint != endpoint.String() || state.ObservedAt == nil || state.Server != server.LocalAddr().String() {
		t.Fatalf("incomplete status: %+v", state)
	}
	// A real WireGuard handshake and ICMP packet still traverse the same bind.
	peerPriv, peerPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	peer, err := wgnode.New(wgnode.Config{PrivateKeyHex: peerPriv, Netstack: true, LocalAddress: netip.MustParseAddr("10.77.0.2")})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := node.AddPeer(peerPub, netip.MustParsePrefix("10.77.0.2/32"), nil); err != nil {
		t.Fatal(err)
	}
	if err := peer.AddPeer(pub, netip.MustParsePrefix("10.77.0.1/32"), &endpoint); err != nil {
		t.Fatal(err)
	}
	if ok, err := peer.Ping(netip.MustParseAddr("10.77.0.1"), 2*time.Second); err != nil || !ok {
		t.Fatalf("WireGuard after STUN: %v %v", ok, err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := node.DiscoverEndpoint(cancelled, []string{server.LocalAddr().String()}); err == nil {
		t.Fatal("cancelled discovery succeeded")
	}
	if state := check("unavailable"); state.Endpoint != "" {
		t.Fatal("stale endpoint retained")
	}
}
