package brambled

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/internal/testport"
	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/registry/registrytest"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

func dirRelay(label string, block uint64) registry.Relay {
	return registry.Relay{Label: label, Address: "203.0.113.1:9420", Pin: "sha256/x", Price: "0", DataRelay: true, Block: block}
}

func TestChooseRelaysPrefersTheTailnetRecordThenTheOldest(t *testing.T) {
	dir := []registry.Relay{dirRelay("c", 30), dirRelay("a", 10), dirRelay("b", 20), dirRelay("mine", 90), dirRelay("e", 50)}
	paid := dirRelay("paid", 1)
	paid.Price = "5"
	unpinned := dirRelay("nopin", 2)
	unpinned.Pin = ""
	dir = append(dir, paid, unpinned)

	rdv, data, _ := chooseRelays(dir, []string{"mine", "unknown"})
	var labels []string
	for _, r := range rdv {
		labels = append(labels, r.Label)
	}
	if fmt.Sprint(labels) != "[mine a b c]" {
		t.Fatalf("rendezvous set %v, want the tailnet's relay then the three oldest", labels)
	}
	if _, ok := data["paid"]; ok {
		t.Fatal("a paid relay was chosen")
	}
	if len(data) != 4 {
		t.Fatalf("data pool %v", data)
	}
}

func TestChooseRelaysSkipsRelaysThatKeepFailing(t *testing.T) {
	dir := []registry.Relay{dirRelay("junk1", 1), dirRelay("junk2", 2), dirRelay("good", 9)}
	for i := 0; i < healthStrikes; i++ {
		health.fail("junk1")
		health.fail("junk2")
	}
	defer health.ok("junk1")
	defer health.ok("junk2")
	rdv, _, _ := chooseRelays(dir, nil)
	if len(rdv) != 1 || rdv[0].Label != "good" {
		t.Fatalf("set %v", rdv)
	}
}

// A directory flooded with dead relays listed before the real one: the
// tailnet's relays record still gets both nodes onto the working relay fast.
func TestJunkRelaysInTheDirectoryDoNotStallAConnect(t *testing.T) {
	chain := registrytest.New(t, "junk.net.bramble.eth")
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := closed.Addr().String()
	closed.Close()
	for i := 0; i < 20; i++ {
		chain.EnrollRelay(fmt.Sprintf("junk%02d", i), map[string]string{
			registry.KeyRendezvous: deadAddr, registry.KeyPin: "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			registry.KeyDataRelay: "on", registry.KeyPrice: "0",
		})
	}
	good, _ := startRelay(t, "good")
	chain.EnrollRelay("good", map[string]string{registry.KeyRendezvous: good.Address, registry.KeyPin: good.Pin, registry.KeyDataRelay: "on", registry.KeyPrice: "0"})
	chain.SetTailnetText(registry.KeyTailnetRelays, "good")

	ping, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ping.Close()
	go func() {
		for {
			c, err := ping.Accept()
			if err != nil {
				return
			}
			io.WriteString(c, "pong")
			c.Close()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handles := map[string]chan *Handle{}
	keys := map[string]string{}
	for i, label := range []string{"laptop", "server"} {
		priv, pub, _ := wgnode.GenerateKeyPair()
		keys[label] = priv
		chain.Enroll(label, map[string]string{registry.KeyPubkey: pub, registry.KeyMeshIP: fmt.Sprintf("10.140.0.%d/24", i+1)})
	}
	for _, label := range []string{"laptop", "server"} {
		priv := keys[label]
		tn, err := registry.NewTailnet(chain.Config(""))
		if err != nil {
			t.Fatal(err)
		}
		if err := tn.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		go tn.Run(ctx)
		args := []string{"-label", label, "-private-key", priv, "-netstack", "-status-port", "0",
			"-listen-port", strconv.Itoa(int(testport.FreeUDP(t))), "-ttl", "2s"}
		if label == "laptop" {
			args = append(args, "-relay-first")
		} else {
			args = append(args, "-service", fmt.Sprintf("ping=%d/open", ping.Addr().(*net.TCPAddr).Port))
		}
		ch := make(chan *Handle, 1)
		handles[label] = ch
		go func() { _ = Serve(ctx, tn, args, func(h *Handle) { ch <- h }) }()
	}
	laptop := <-handles["laptop"]
	<-handles["server"]
	start := time.Now()
	for time.Since(start) < 30*time.Second {
		if c, err := laptop.Dial("server", "ping"); err == nil {
			c.Close()
			t.Logf("connected through the good relay after %s despite 20 dead directory entries", time.Since(start).Round(10*time.Millisecond))
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("dead directory relays stalled the connection")
}

// ADR 0020: a node started before any relay was listed must pick up the first
// relay without a restart. With no relay at start-up, neither node has a data
// relay or a rendezvous relay; loopback candidates are refused outside tests,
// so the only way the two can connect is through the relay listed later.
func TestRelayListedAfterStartIsUsedWithoutRestart(t *testing.T) {
	old := relayRefreshInterval
	relayRefreshInterval = 200 * time.Millisecond
	t.Cleanup(func() { relayRefreshInterval = old })

	chain := registrytest.New(t, "late.net.bramble.eth")
	ping, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ping.Close()
	go func() {
		for {
			c, err := ping.Accept()
			if err != nil {
				return
			}
			io.WriteString(c, "pong")
			c.Close()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handles := map[string]chan *Handle{}
	keys := map[string]string{}
	for i, label := range []string{"laptop", "server"} {
		priv, pub, _ := wgnode.GenerateKeyPair()
		keys[label] = priv
		chain.Enroll(label, map[string]string{registry.KeyPubkey: pub, registry.KeyMeshIP: fmt.Sprintf("10.150.0.%d/24", i+1)})
	}
	for _, label := range []string{"laptop", "server"} {
		tn, err := registry.NewTailnet(chain.Config(""))
		if err != nil {
			t.Fatal(err)
		}
		if err := tn.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		go tn.Run(ctx)
		args := []string{"-label", label, "-private-key", keys[label], "-netstack", "-status-port", "0",
			"-listen-port", strconv.Itoa(int(testport.FreeUDP(t))), "-ttl", "2s"}
		if label == "server" {
			args = append(args, "-service", fmt.Sprintf("ping=%d/open", ping.Addr().(*net.TCPAddr).Port))
		}
		ch := make(chan *Handle, 1)
		handles[label] = ch
		go func() { _ = Serve(ctx, tn, args, func(h *Handle) { ch <- h }) }()
	}
	laptop := <-handles["laptop"]
	<-handles["server"]

	if c, err := laptop.Dial("server", "ping"); err == nil {
		c.Close()
		t.Fatal("connected with no relay listed; the test no longer exercises a late relay")
	}

	relay, _ := startRelay(t, "late")
	chain.EnrollRelay("late", map[string]string{registry.KeyRendezvous: relay.Address, registry.KeyPin: relay.Pin, registry.KeyDataRelay: "on", registry.KeyPrice: "0"})
	start := time.Now()
	for time.Since(start) < 45*time.Second {
		if c, err := laptop.Dial("server", "ping"); err == nil {
			body, _ := io.ReadAll(c)
			c.Close()
			if string(body) != "pong" {
				t.Fatalf("ping answered %q", body)
			}
			t.Logf("connected through a relay listed after start-up, %s after listing it", time.Since(start).Round(10*time.Millisecond))
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("a relay listed after start-up was never used")
}
