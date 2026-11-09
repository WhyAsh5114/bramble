package daemon

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/registry/registrytest"
	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

func freeUDP(t *testing.T) uint16 {
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return uint16(c.LocalAddr().(*net.UDPAddr).Port)
}

func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Two daemons on a fake chain: each waits with a join code until enrolled,
// then runs its node, finds the relay in the on-chain directory, and pings
// the other through it.
func TestTwoDaemonsJoinAndPingThroughTheDirectoryRelay(t *testing.T) {
	chain := registrytest.New(t, "e2e.net.bramble.eth")
	relay, err := relaysrv.StartLocal(relaysrv.Limits{SessionBytes: 1 << 22, SessionTTL: time.Minute, MaxSessions: 8, SessionsPerHour: 50, PortMin: 49100, PortMax: 49199}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	chain.EnrollRelay("r1", map[string]string{
		registry.KeyRendezvous: relay.Addr, registry.KeyPin: relay.Pin, registry.KeyDataRelay: "on", registry.KeyPrice: "0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemons := map[string]*Daemon{}
	for _, label := range []string{"alpha", "beta"} {
		store := &Store{Dir: t.TempDir()}
		if err := store.Ensure(); err != nil {
			t.Fatal(err)
		}
		saved := Saved{Label: label, Tailnet: &TailnetRef{Name: chain.Suffix, Registry: chain.Registry, Resolver: chain.Resolver, Start: chain.Start}, RelayFirst: label == "alpha"}
		if err := store.SaveSaved(saved); err != nil {
			t.Fatal(err)
		}
		d := &Daemon{Store: store, Network: chain.Network(), RPCOverride: []string{chain.URL}, ListenPort: freeUDP(t), Version: "test",
			Logger: log.New(io.Discard, "", 0)}
		if os.Getenv("BRAMBLE_TRACE") != "" {
			d.Logger = log.New(os.Stderr, label+": ", 0)
		}
		if err := d.Init(); err != nil {
			t.Fatal(err)
		}
		daemons[label] = d
		go d.Run(ctx)
	}
	for label, d := range daemons {
		waitFor(t, label+" to wait for enrollment", 20*time.Second, func() bool { return d.State().Phase == PhaseWaiting })
		st := d.State()
		code, err := ParseJoinCode(st.JoinCode)
		if err != nil || code.Label != label || code.Pubkey != st.PublicKey {
			t.Fatalf("%s join code %q: %+v %v", label, st.JoinCode, code, err)
		}
	}
	// The admin enrolls both from their join codes.
	for i, label := range []string{"alpha", "beta"} {
		code, _ := ParseJoinCode(daemons[label].State().JoinCode)
		chain.Enroll(code.Label, map[string]string{registry.KeyPubkey: code.Pubkey, registry.KeyMeshIP: fmt.Sprintf("10.77.0.%d/24", i+1)})
	}
	for label, d := range daemons {
		waitFor(t, label+" to run its node", 30*time.Second, func() bool {
			st := d.State()
			return st.Phase == PhaseMember && st.Node != nil && st.Node.Running
		})
	}

	alpha := daemons["alpha"]
	var res *PingResult
	waitFor(t, "alpha to ping beta", 40*time.Second, func() bool {
		res, err = alpha.Ping("beta")
		return err == nil
	})
	if !strings.Contains(string(res.Answer), `"pong":"beta"`) {
		t.Fatalf("ping answer %s", res.Answer)
	}
	t.Logf("alpha -> beta ping: %.1f ms over %s path", res.Millis, res.Path)

	services, err := alpha.PeerServices("beta")
	if err != nil || len(services) != 1 || services[0].Name != PingService {
		t.Fatalf("beta's services seen by alpha: %+v %v", services, err)
	}

	fwd, err := alpha.Forward("beta", PingService)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", fwd.Port))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"pong":"beta"`) {
		t.Fatalf("forwarded answer %s", body)
	}

	devs, err := alpha.Devices()
	if err != nil || len(devs) != 2 {
		t.Fatalf("devices %+v %v", devs, err)
	}

	// Revocation stops beta's node.
	chain.SetText("beta", registry.KeyRevoked, "true")
	waitFor(t, "beta to see its revocation", 20*time.Second, func() bool { return daemons["beta"].State().Phase == PhaseRevoked })
}

func TestJoinCodeRoundTripAndRejectsGarbage(t *testing.T) {
	j := JoinCode{Tailnet: "home.net.bramble.eth", Label: "laptop", Pubkey: strings.Repeat("ab", 32)}
	got, err := ParseJoinCode(j.Encode())
	if err != nil || got != j {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"", "hello", "bramble-join:1:!!!", JoinCode{Label: "Bad Name", Pubkey: j.Pubkey}.Encode(), JoinCode{Label: "ok", Pubkey: "zz"}.Encode()} {
		if _, err := ParseJoinCode(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
