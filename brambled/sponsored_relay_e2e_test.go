package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
	"github.com/WhyAsh5114/bramble/brambled/sponsor"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startRealSponsoringRelay builds and runs the real relay binary with
// -meter -data-relay -sponsor-enrolled. The relay spawns the test harness
// (relay-sidecar/test/e2e-harness) in place of relay-sidecar/src/index.ts:
// the sponsor routes, token minting and the relay's internal allocation API
// are the real code; only the registry read is a fixture, and there is no
// x402 middleware, so nothing can reach Hedera.
func startRealSponsoringRelay(t *testing.T, enrolled string, sponsorBytes int64) (rendezvousAddr, sidecarURL string) {
	t.Helper()
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not on PATH; the relay spawns its sidecar with bun")
	}
	bin := filepath.Join(t.TempDir(), "relay")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "../relay"
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building relay: %v\n%s", err, out)
	}

	rdvPort, sidecarPort, internalPort := freePort(t), freePort(t), freePort(t)
	cmd := exec.Command(bin,
		"-addr", fmt.Sprintf("127.0.0.1:%d", rdvPort),
		"-meter", "-payee", "0.0.1",
		"-sidecar-dir", "../relay-sidecar/test/e2e-harness",
		"-sidecar-port", fmt.Sprint(sidecarPort),
		"-data-relay", "-data-host", "127.0.0.1",
		"-internal-port", fmt.Sprint(internalPort),
		"-sponsor-enrolled", "-sponsor-bytes", fmt.Sprint(sponsorBytes),
	)
	cmd.Env = append(os.Environ(),
		"BRAMBLE_TAILNET_NAME=fixture.eth",
		"BRAMBLE_TAILNET_REGISTRY=0x0000000000000000000000000000000000000001",
		"HARNESS_ENROLLED_LABELS="+enrolled,
	)
	var logs strings.Builder
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting relay: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if t.Failed() {
			t.Logf("relay output:\n%s", logs.String())
		}
	})
	sidecarURL = fmt.Sprintf("http://127.0.0.1:%d", sidecarPort)
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(sidecarURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("relay sidecar not healthy within 30s\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Sprintf("127.0.0.1:%d", rdvPort), sidecarURL
}

func TestSponsoredRelay_RefusesUnenrolledLabel(t *testing.T) {
	_, sidecarURL := startRealSponsoringRelay(t, "device1", 65536)
	for _, label := range []string{"stranger", "Not_A_Label"} {
		p := &sponsor.Payer{Label: label}
		if _, err := p.RendezvousToken(sidecarURL); err == nil {
			t.Fatalf("token for %q: expected refusal", label)
		}
		if _, err := p.DataRelaySession(sidecarURL, 1<<30); err == nil {
			t.Fatalf("session for %q: expected refusal", label)
		}
	}
	// Asking for far more than the cap still yields exactly the cap.
	granted, err := (&sponsor.Payer{Label: "device1"}).DataRelaySession(sidecarURL, 1<<30)
	if err != nil {
		t.Fatalf("enrolled device1: %v", err)
	}
	if granted.Bytes != 65536 || granted.Port == 0 || granted.SettlementTxID != "" {
		t.Fatalf("expected a 65536-byte unpaid session, got %+v", granted)
	}
}

// sponsoredPair is a "device" node with no Hedera key and a "cloud" node,
// neither given any endpoint for the other, joined only through the real
// relay binary with sponsored tokens and a sponsored session for the device.
type sponsoredPair struct {
	device, cloud       *wgnode.Node
	devicePub, cloudPub string
	deviceIP, cloudIP   netip.Addr
	relays              []sidecar.RendezvousRelay
	pool                dataRelayFlag
	deviceEndpoint      *netip.AddrPort
}

func connectSponsoredPair(t *testing.T, sponsorBytes int64, handler http.Handler) *sponsoredPair {
	t.Helper()
	rdvAddr, sidecarURL := startRealSponsoringRelay(t, "device1,cloud1", sponsorBytes)
	p := &sponsoredPair{
		relays:   []sidecar.RendezvousRelay{{Label: "relay1", Address: rdvAddr, SidecarURL: sidecarURL}},
		pool:     dataRelayFlag{"relay1": sidecarURL},
		deviceIP: netip.MustParseAddr("10.99.0.1"),
		cloudIP:  netip.MustParseAddr("10.99.0.2"),
	}
	var devicePriv, cloudPriv string
	var err error
	if devicePriv, p.devicePub, err = wgnode.GenerateKeyPair(); err != nil {
		t.Fatal(err)
	}
	if cloudPriv, p.cloudPub, err = wgnode.GenerateKeyPair(); err != nil {
		t.Fatal(err)
	}
	if p.device, err = wgnode.New(wgnode.Config{PrivateKeyHex: devicePriv, ListenPort: 0, LocalAddress: p.deviceIP, Netstack: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.device.Close() })
	if p.cloud, err = wgnode.New(wgnode.Config{PrivateKeyHex: cloudPriv, ListenPort: 0, LocalAddress: p.cloudIP, Netstack: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.cloud.Close() })

	ln, err := p.cloud.ListenTCP(8080)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() { _ = http.Serve(ln, handler) }()

	// Device side: the flagged -relay-peer, sponsored. Cloud side: the
	// adopting peer; it only needs a sponsored rendezvous token.
	var cloudEndpoint *netip.AddrPort
	var deviceErr, cloudErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		p.deviceEndpoint, _, deviceErr = relayRoutedEndpoint(p.device, &sponsor.Payer{Label: "device1"}, p.pool, 10_000,
			unrestrictedDataRelayPurchasePolicy, p.relays, p.devicePub, p.cloudPub, 20*time.Second)
	}()
	go func() {
		defer wg.Done()
		candidate, _, err := exchangeAnyMetered(p.relays, &sponsor.Payer{Label: "cloud1"}, p.cloudPub, p.devicePub, "", 20*time.Second)
		if err != nil {
			cloudErr = err
			return
		}
		addr, err := peerEndpointFromCandidate(candidate)
		if err != nil {
			cloudErr = err
			return
		}
		cloudErr = p.cloud.SetPersistentKeepalive(p.devicePub, dataRelayKeepaliveSeconds)
		cloudEndpoint = &addr
	}()
	wg.Wait()
	if deviceErr != nil || cloudErr != nil {
		t.Fatalf("device: %v; cloud: %v", deviceErr, cloudErr)
	}
	if p.deviceEndpoint.String() != cloudEndpoint.String() {
		t.Fatalf("endpoints differ: %s vs %s", p.deviceEndpoint, cloudEndpoint)
	}
	if err := p.device.AddPeer(p.cloudPub, netip.PrefixFrom(p.cloudIP, 32), p.deviceEndpoint); err != nil {
		t.Fatal(err)
	}
	if err := p.cloud.AddPeer(p.devicePub, netip.PrefixFrom(p.deviceIP, 32), cloudEndpoint); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *sponsoredPair) httpClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return p.device.DialTCP(netip.AddrPortFrom(p.cloudIP, 8080))
		}},
	}
}

// getUntil retries a GET until it returns or the deadline passes.
func getUntil(client *http.Client, url string, within time.Duration) (int, string, error) {
	var lastErr error
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return resp.StatusCode, string(body), nil
	}
	return 0, "", lastErr
}

// TestSponsoredRelay_HTTP200ThroughRelayWithNoDirectPath is ETH-189's
// end-to-end check: a device with no Hedera key reaches a cloud node only
// through the real relay binary, using sponsored tokens and a sponsored
// session.
func TestSponsoredRelay_HTTP200ThroughRelayWithNoDirectPath(t *testing.T) {
	p := connectSponsoredPair(t, 1<<20, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello through the sponsored relay")
	}))
	status, body, err := getUntil(p.httpClient(20*time.Second), "http://cloud1.mesh/", 20*time.Second)
	if err != nil {
		t.Fatalf("HTTP GET through the relay: %v", err)
	}
	t.Logf("device -> relay %s -> cloud: HTTP %d %q", p.deviceEndpoint, status, body)
	if status != http.StatusOK || body != "hello through the sponsored relay" {
		t.Fatalf("unexpected response: %d %q", status, body)
	}
}

// countingPayer counts the tokens and data-relay sessions a payer takes.
type countingPayer struct {
	relayPayer
	tokens, sessions atomic.Int32
}

func (c *countingPayer) RendezvousToken(sidecarURL string) (*sidecar.RendezvousTokenResponse, error) {
	resp, err := c.relayPayer.RendezvousToken(sidecarURL)
	if err == nil {
		c.tokens.Add(1)
	}
	return resp, err
}

func (c *countingPayer) DataRelaySession(sidecarURL string, bytes int64) (*sidecar.DataRelaySessionResponse, error) {
	resp, err := c.relayPayer.DataRelaySession(sidecarURL, bytes)
	if err == nil {
		c.sessions.Add(1)
	}
	return resp, err
}

// TestSponsoredRelay_RenewsSessionAtCapWithOneRelay is ETH-195's renewal
// finding: with a single relay, a sponsored session that reaches its byte
// cap must be replaced by a new one, not end the connection. The cap is
// 64 KiB; a 512 KiB download exhausts it. The device's failover watcher and
// the cloud's paced, stall-gated adopt watcher run as runServe wires them.
// A session's TTL expiry stops forwarding the same way, so the same path
// renews it; this test does not wait out the 5-minute TTL.
func TestSponsoredRelay_RenewsSessionAtCapWithOneRelay(t *testing.T) {
	big := strings.Repeat("x", 512<<10)
	p := connectSponsoredPair(t, 64<<10, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			fmt.Fprint(w, big)
			return
		}
		fmt.Fprint(w, "hello after renewal")
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	devicePayer := &countingPayer{relayPayer: &sponsor.Payer{Label: "device1"}}
	devicePayer.sessions.Store(1) // the session connectSponsoredPair took
	state := newRelayState()
	state.set("cloud1", p.cloudPub, "relay1")
	pace, err := sponsor.PollLimiter(30)
	if err != nil {
		t.Fatal(err)
	}
	go watchDataRelayFailover(ctx, p.device, devicePayer, state, "cloud1", netip.PrefixFrom(p.cloudIP, 32), p.pool, 10_000,
		unrestrictedDataRelayPurchasePolicy, staticRendezvous(p.relays), p.devicePub, 20*time.Second)
	cloudPayer := &countingPayer{relayPayer: &sponsor.Payer{Label: "cloud1"}}
	go watchDataRelayAdopt(ctx, p.cloud, cloudPayer, "device1", p.cloudPub, p.devicePub, "",
		netip.PrefixFrom(p.deviceIP, 32), staticRendezvous(p.relays), pace)

	if status, body, err := getUntil(p.httpClient(10*time.Second), "http://cloud1.mesh/", 20*time.Second); err != nil || status != http.StatusOK {
		t.Fatalf("first GET: %d %q %v", status, body, err)
	}
	// On a healthy link the paced adopter must not poll: the unpaced loop
	// takes a token every 3 s and used the default 30/hour in 90 s.
	time.Sleep(15 * time.Second)
	if n := cloudPayer.tokens.Load(); n != 0 {
		t.Fatalf("cloud took %d sponsored tokens on a healthy link", n)
	}
	// The download runs into the cap; it may fail or stall, which is the point.
	if _, body, err := getUntil(p.httpClient(8*time.Second), "http://cloud1.mesh/big", 8*time.Second); err == nil && len(body) == len(big) {
		t.Fatalf("a %d-byte body crossed a 64 KiB session; the cap did not apply", len(body))
	}

	deadline := time.Now().Add(90 * time.Second)
	for devicePayer.sessions.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
	}
	if n := devicePayer.sessions.Load(); n < 2 {
		t.Fatalf("no new sponsored session after the cap (sessions taken: %d)", n)
	}
	status, body, err := getUntil(p.httpClient(10*time.Second), "http://cloud1.mesh/", 60*time.Second)
	if err != nil {
		t.Fatalf("GET after renewal: %v", err)
	}
	t.Logf("after the cap: %d sponsored sessions, %d cloud tokens, HTTP %d %q", devicePayer.sessions.Load(), cloudPayer.tokens.Load(), status, body)
	if status != http.StatusOK || body != "hello after renewal" {
		t.Fatalf("unexpected response after renewal: %d %q", status, body)
	}
}
