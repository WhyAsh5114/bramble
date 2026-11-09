package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
	"github.com/WhyAsh5114/bramble/brambled/sponsor"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// Exercise the real relay and device endpoint resolver with two cloud-side
// exchanges under one WireGuard key. The cloud exchanges use the public wire
// protocol directly so registration order is deterministic, without sleeps.
// Registry admission is a fixture; WireGuard and the HTTP transfer are real.
func TestSponsoredRelay_ConcurrentGatewayExchangesReachFreshDevice(t *testing.T) {
	rdv, paymentURL := startRealSponsoringRelay(t, "device1,cloud1", 1<<20)
	devicePriv, devicePub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	cloudPriv, cloudPub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	p := &sponsoredPair{deviceIP: netip.MustParseAddr("10.101.0.1"), cloudIP: netip.MustParseAddr("10.101.0.2")}
	p.device, err = wgnode.New(wgnode.Config{PrivateKeyHex: devicePriv, LocalAddress: p.deviceIP, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.device.Close()
	p.cloud, err = wgnode.New(wgnode.Config{PrivateKeyHex: cloudPriv, LocalAddress: p.cloudIP, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.cloud.Close()
	type wireMessage struct {
		Type      string `json:"type"`
		Pubkey    string `json:"pubkey,omitempty"`
		To        string `json:"to,omitempty"`
		From      string `json:"from,omitempty"`
		Candidate string `json:"candidate,omitempty"`
		Token     string `json:"token,omitempty"`
	}
	register := func(remote string) (net.Conn, *json.Decoder) {
		t.Helper()
		token, err := (&sponsor.Payer{Label: "cloud1"}).RendezvousToken(paymentURL)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := net.DialTimeout("tcp", rdv, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		enc, dec := json.NewEncoder(conn), json.NewDecoder(conn)
		if err := enc.Encode(wireMessage{Type: "hello", Pubkey: cloudPub, To: remote, Token: token.Token}); err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(wireMessage{Type: "offer", To: "absent-barrier"}); err != nil {
			t.Fatal(err)
		}
		var reply wireMessage
		if err := dec.Decode(&reply); err != nil || reply.Type != "error" {
			t.Fatalf("registration barrier: type=%s error=%v", reply.Type, err)
		}
		return conn, dec
	}
	cloudConn, cloudDecoder := register(devicePub)
	register("offline-peer") // must not replace the fresh device's exchange
	type adoptedEndpoint struct {
		endpoint netip.AddrPort
		err      error
	}
	cloudResult := make(chan adoptedEndpoint, 1)
	go func() {
		var offer wireMessage
		if err := cloudDecoder.Decode(&offer); err != nil {
			cloudResult <- adoptedEndpoint{err: err}
			return
		}
		if offer.Type != "offer" || offer.From != devicePub {
			cloudResult <- adoptedEndpoint{err: fmt.Errorf("wrong offer type or sender")}
			return
		}
		endpoint, err := announcedAddr(offer.Candidate)
		if err == nil {
			err = json.NewEncoder(cloudConn).Encode(wireMessage{Type: "offer", To: devicePub})
		}
		cloudResult <- adoptedEndpoint{endpoint: endpoint, err: err}
	}()
	relays := []sidecar.RendezvousRelay{{Label: "relay1", Address: rdv, SidecarURL: paymentURL}}
	endpoint, _, err := relayRoutedEndpoint(p.device, &sponsor.Payer{Label: "device1"}, dataRelayFlag{"relay1": paymentURL},
		10_000, unrestrictedDataRelayPurchasePolicy, relays, devicePub, cloudPub, 3*time.Second, nil)
	if err != nil {
		t.Fatalf("approved fresh device cannot learn a relay endpoint with concurrent gateway exchanges: %v", err)
	}
	cloudEndpoint := <-cloudResult
	if cloudEndpoint.err != nil {
		t.Fatal(cloudEndpoint.err)
	}
	if err := p.device.AddPeer(cloudPub, netip.PrefixFrom(p.cloudIP, 32), endpoint); err != nil {
		t.Fatal(err)
	}
	if err := p.cloud.SetPersistentKeepalive(devicePub, dataRelayKeepaliveSeconds); err != nil {
		t.Fatal(err)
	}
	if err := p.cloud.AddPeer(devicePub, netip.PrefixFrom(p.deviceIP, 32), &cloudEndpoint.endpoint); err != nil {
		t.Fatal(err)
	}
	ln, err := p.cloud.ListenTCP(8080)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "fresh device reached gateway") }))
	}()
	status, body, err := getUntil(p.httpClient(3*time.Second), "http://cloud1.mesh/", 10*time.Second)
	if err != nil || status != http.StatusOK || body != "fresh device reached gateway" {
		t.Fatalf("fresh device HTTP: status=%d body=%q error=%v", status, body, err)
	}
}
