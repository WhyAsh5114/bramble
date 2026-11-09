package brambled

import (
	"context"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

var nextRelayPort atomic.Int32

func init() { nextRelayPort.Store(48000) }

// startRelay runs the real relay on loopback with its own UDP port range.
func startRelay(t *testing.T, label string) (rendezvous.Relay, *relaysrv.Local) {
	t.Helper()
	lo := int(nextRelayPort.Add(100)) - 100
	l, err := relaysrv.StartLocal(relaysrv.Limits{
		SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 16, SessionsPerHour: 100, PortMin: lo, PortMax: lo + 99,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return rendezvous.Relay{Label: label, Address: l.Addr, Pin: l.Pin}, l
}

type countingSessions struct{ n atomic.Int32 }

func (c *countingSessions) Session(r rendezvous.Relay, bytes int64) (*rendezvous.SessionGrant, error) {
	c.n.Add(1)
	return freeSessions{}.Session(r, bytes)
}

// testNode is a netstack WireGuard node with a registered rendezvous identity.
func testNode(t *testing.T, addr string) (*wgnode.Node, string, netip.Addr) {
	t.Helper()
	priv, pub, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rendezvous.RegisterIdentity(priv); err != nil {
		t.Fatal(err)
	}
	a := netip.MustParseAddr(addr)
	n, err := wgnode.New(wgnode.Config{PrivateKeyHex: priv, LocalAddress: a, Netstack: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Close)
	return n, pub, a
}

func pingUntil(t *testing.T, from *wgnode.Node, to netip.Addr, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ok, err := from.Ping(to, 300*time.Millisecond); err == nil && ok {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// A device with no direct path reaches a cloud node through a bound data
// session; the session secret travels only inside the sealed candidate.
func TestRelayRoutedPeersTalkThroughABoundSession(t *testing.T) {
	relay, _ := startRelay(t, "relay1")
	device, devicePub, deviceAddr := testNode(t, "10.120.0.1")
	cloud, cloudPub, cloudAddr := testNode(t, "10.120.0.2")
	relays := []rendezvous.Relay{relay}

	cloudSide := make(chan error, 1)
	go func() {
		candidate, _, err := exchangeAny(relays, cloudPub, devicePub, "", 10*time.Second)
		if err != nil {
			cloudSide <- err
			return
		}
		endpoint, err := peerEndpointFromCandidate(candidate)
		if err != nil {
			cloudSide <- err
			return
		}
		adoptBind(cloud, candidate, endpoint)
		cloudSide <- cloud.AddPeer(devicePub, netip.PrefixFrom(deviceAddr, 32), &endpoint)
	}()

	sessions := &countingSessions{}
	ep, session, err := relayRoutedEndpoint(device, sessions, dataRelayFlag{"relay1": relay}, 10_000, relays, devicePub, cloudPub, 10*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := device.AddPeer(cloudPub, netip.PrefixFrom(cloudAddr, 32), ep); err != nil {
		t.Fatal(err)
	}
	if err := <-cloudSide; err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if !pingUntil(t, device, cloudAddr, 40*time.Second) {
		dp, _ := device.Peers()
		cp, _ := cloud.Peers()
		t.Fatalf("no traffic through the relay session: device %+v cloud %+v", dp, cp)
	}
	t.Logf("connected after %s", time.Since(start))
	if sessions.n.Load() != 1 || session.grantedBytes != 10_000 {
		t.Fatalf("sessions=%d granted=%d", sessions.n.Load(), session.grantedBytes)
	}
}

// When the relay a session runs through dies, the holder moves to the backup
// relay and the peer adopts the new session.
func TestFailoverMovesToTheBackupRelay(t *testing.T) {
	primary, primaryLocal := startRelay(t, "primary")
	backup, _ := startRelay(t, "backup")
	// Rendezvous runs through the backup only, so killing the primary leaves signaling intact.
	relays := []rendezvous.Relay{backup}
	device, devicePub, deviceAddr := testNode(t, "10.121.0.1")
	cloud, cloudPub, cloudAddr := testNode(t, "10.121.0.2")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cloudReady := make(chan error, 1)
	go func() {
		candidate, _, err := exchangeAny(relays, cloudPub, devicePub, "", 10*time.Second)
		if err == nil {
			var endpoint netip.AddrPort
			if endpoint, err = peerEndpointFromCandidate(candidate); err == nil {
				adoptBind(cloud, candidate, endpoint)
				err = cloud.AddPeer(devicePub, netip.PrefixFrom(deviceAddr, 32), &endpoint)
			}
		}
		cloudReady <- err
	}()
	pool := dataRelayFlag{"primary": primary}
	ep, session, err := relayRoutedEndpoint(device, freeSessions{}, pool, 1<<20, relays, devicePub, cloudPub, 10*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := device.AddPeer(cloudPub, netip.PrefixFrom(cloudAddr, 32), ep); err != nil {
		t.Fatal(err)
	}
	if err := <-cloudReady; err != nil {
		t.Fatal(err)
	}
	if !pingUntil(t, device, cloudAddr, 10*time.Second) {
		t.Fatal("initial relay path did not connect")
	}

	state := newRelayState()
	state.set("cloud", cloudPub, session)
	pool["backup"] = backup
	source := staticRendezvous(relays)
	go watchDataRelayFailover(ctx, device, freeSessions{}, state, "cloud", pool, 1<<20, source, devicePub, 3*time.Second)
	go watchDataRelayAdopt(ctx, cloud, "device", cloudPub, devicePub, "", netip.PrefixFrom(deviceAddr, 32), source)

	primaryLocal.Close()
	if !pingUntil(t, device, cloudAddr, 40*time.Second) {
		t.Fatal("traffic did not resume through the backup relay")
	}
	if _, used, _ := state.get("cloud"); used != "backup" {
		t.Fatalf("session is on %q, want backup", used)
	}
}

func TestDataRelayEndpointUpdatePreservesAdmission(t *testing.T) {
	node, _, _ := testNode(t, "10.100.0.1")
	_, peerKey, err := wgnode.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	endpoint := netip.MustParseAddrPort("127.0.0.1:9")
	route := netip.MustParsePrefix("10.100.0.2/32")
	if err := node.AddPeer(peerKey, route, nil); err != nil {
		t.Fatal(err)
	}
	if err := node.UpdatePeerEndpoint(peerKey, endpoint, dataRelayKeepaliveSeconds); err != nil {
		t.Fatal(err)
	}
	peers, _ := node.Peers()
	if len(peers) != 1 || peers[0].Endpoint != endpoint.String() || len(peers[0].AllowedIPs) != 1 || peers[0].AllowedIPs[0] != route.String() {
		t.Fatalf("transport update changed admission: %+v", peers)
	}
	if err := node.RemovePeer(peerKey); err != nil {
		t.Fatal(err)
	}
	if err := node.UpdatePeerEndpoint(peerKey, endpoint, dataRelayKeepaliveSeconds); err != nil {
		t.Fatal(err)
	}
	if peers, _ := node.Peers(); len(peers) != 0 {
		t.Fatalf("transport update restored a revoked peer: %+v", peers)
	}
}
