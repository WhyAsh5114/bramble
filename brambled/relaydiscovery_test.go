package brambled

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
)

func TestExchangeBackoffDoublesAndResets(t *testing.T) {
	now := time.Unix(1000, 0)
	b := newExchangeBackoff(30*time.Second, 2*time.Minute)
	b.now = func() time.Time { return now }

	if b.Wait("peer") != 0 {
		t.Fatal("a peer with no failures must not wait")
	}
	want := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second, 120 * time.Second}
	for i, w := range want {
		b.Fail("peer")
		if got := b.Wait("peer"); got != w {
			t.Fatalf("after failure %d: wait %s, want %s", i+1, got, w)
		}
		now = now.Add(w) // the wait elapses
		if b.Wait("peer") != 0 {
			t.Fatalf("after failure %d: still waiting once the wait elapsed", i+1)
		}
	}
	if b.Wait("other") != 0 {
		t.Fatal("another peer must not inherit the backoff")
	}
	b.Fail("peer")
	b.Succeed("peer")
	if b.Wait("peer") != 0 {
		t.Fatal("a success must clear the backoff")
	}
	b.Fail("peer")
	if got := b.Wait("peer"); got != 30*time.Second {
		t.Fatalf("after reset the first failure waits %s, want 30s", got)
	}
}

func liveRelay(label string) rendezvous.Relay {
	return rendezvous.Relay{Label: label, Address: label + ".example:9420", Pin: "sha256/" + label}
}

func TestRelayDirectoryAddsRelaysThatAppearLater(t *testing.T) {
	var found []rendezvous.Relay
	data := dataRelayFlag{}
	var stun []string
	fetch := func() ([]rendezvous.Relay, dataRelayFlag, []string, error) { return found, data, stun, nil }
	dir := newRelayDirectory(fetch, relaySets{})

	if err := dir.refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.Rendezvous(); err == nil {
		t.Fatal("Rendezvous with an empty directory: want an error")
	}
	if len(dir.DataRelays()) != 0 || len(dir.STUN()) != 0 {
		t.Fatalf("empty directory gave data=%v stun=%v", dir.DataRelays(), dir.STUN())
	}

	found = []rendezvous.Relay{liveRelay("mumbai")}
	data = dataRelayFlag{"mumbai": liveRelay("mumbai")}
	stun = []string{"mumbai.example:3478"}
	if err := dir.refresh(); err != nil {
		t.Fatal(err)
	}
	rdv, err := dir.Rendezvous()
	if err != nil || len(rdv) != 1 || rdv[0].Label != "mumbai" {
		t.Fatalf("Rendezvous after relay appeared: %v %v", rdv, err)
	}
	if got := dir.DataRelays(); len(got) != 1 || got["mumbai"].Address == "" {
		t.Fatalf("DataRelays after relay appeared: %v", got)
	}
	if got := dir.STUN(); len(got) != 1 || got[0] != "mumbai.example:3478" {
		t.Fatalf("STUN after relay appeared: %v", got)
	}
}

func TestRelayDirectoryNeverRemovesAndDedupes(t *testing.T) {
	found := []rendezvous.Relay{liveRelay("a")}
	stun := []string{"a:3478"}
	fetch := func() ([]rendezvous.Relay, dataRelayFlag, []string, error) {
		return found, dataRelayFlag{}, stun, nil
	}
	dir := newRelayDirectory(fetch, relaySets{})
	_ = dir.refresh()
	found = []rendezvous.Relay{liveRelay("b"), liveRelay("a")}
	stun = []string{"b:3478", "a:3478"}
	_ = dir.refresh()
	found, stun = nil, nil
	_ = dir.refresh()

	rdv, _ := dir.Rendezvous()
	if len(rdv) != 2 || rdv[0].Label != "a" || rdv[1].Label != "b" {
		t.Fatalf("Rendezvous = %v, want [a b] in the order found", rdv)
	}
	if got := dir.STUN(); len(got) != 2 || got[0] != "a:3478" || got[1] != "b:3478" {
		t.Fatalf("STUN = %v, want [a:3478 b:3478]", got)
	}
}

func TestRelayDirectoryCapsEachSet(t *testing.T) {
	var found []rendezvous.Relay
	data := dataRelayFlag{}
	var stun []string
	for i := 0; i < 10; i++ {
		r := liveRelay(string(rune('a' + i)))
		found = append(found, r)
		data[r.Label] = r
		stun = append(stun, r.Label+":3478")
	}
	dir := newRelayDirectory(func() ([]rendezvous.Relay, dataRelayFlag, []string, error) {
		return found, data, stun, nil
	}, relaySets{})
	_ = dir.refresh()
	rdv, _ := dir.Rendezvous()
	if len(rdv) != maxRendezvousRelays || len(dir.DataRelays()) != maxDataRelays || len(dir.STUN()) != maxRendezvousRelays {
		t.Fatalf("sizes rdv=%d data=%d stun=%d, want %d %d %d", len(rdv), len(dir.DataRelays()), len(dir.STUN()), maxRendezvousRelays, maxDataRelays, maxRendezvousRelays)
	}
}

func TestRelayDirectoryLeavesFixedSetsAlone(t *testing.T) {
	fetch := func() ([]rendezvous.Relay, dataRelayFlag, []string, error) {
		return []rendezvous.Relay{liveRelay("dir")}, dataRelayFlag{"dir": liveRelay("dir")}, []string{"dir:3478"}, nil
	}
	static := relaySets{
		Rendezvous: []rendezvous.Relay{liveRelay("flag")}, FixedRendezvous: true,
		Data: dataRelayFlag{"flag": liveRelay("flag")}, FixedData: true,
		STUN: nil, FixedSTUN: true, // -no-stun
	}
	dir := newRelayDirectory(fetch, static)
	_ = dir.refresh()
	rdv, _ := dir.Rendezvous()
	if len(rdv) != 1 || rdv[0].Label != "flag" {
		t.Fatalf("Rendezvous = %v, want only the -rendezvous flag", rdv)
	}
	if got := dir.DataRelays(); len(got) != 1 || got["flag"].Label != "flag" {
		t.Fatalf("DataRelays = %v, want only the -data-relay flag", got)
	}
	if got := dir.STUN(); len(got) != 0 {
		t.Fatalf("STUN = %v, want none with -no-stun", got)
	}
}

func TestRelayDirectoryKeepsStateWhenFetchFails(t *testing.T) {
	fail := false
	fetch := func() ([]rendezvous.Relay, dataRelayFlag, []string, error) {
		if fail {
			return nil, nil, nil, errors.New("registry unreachable")
		}
		return []rendezvous.Relay{liveRelay("a")}, dataRelayFlag{"a": liveRelay("a")}, []string{"a:3478"}, nil
	}
	dir := newRelayDirectory(fetch, relaySets{})
	_ = dir.refresh()
	fail = true
	if err := dir.refresh(); err == nil {
		t.Fatal("refresh: want the fetch error")
	}
	if rdv, err := dir.Rendezvous(); err != nil || len(rdv) != 1 {
		t.Fatalf("Rendezvous after a failed refresh = %v %v, want [a]", rdv, err)
	}
	if len(dir.DataRelays()) != 1 || len(dir.STUN()) != 1 {
		t.Fatal("a failed refresh dropped data relays or STUN servers")
	}
}

func TestRelayDirectoryReturnsCopies(t *testing.T) {
	dir := newRelayDirectory(func() ([]rendezvous.Relay, dataRelayFlag, []string, error) {
		return []rendezvous.Relay{liveRelay("a")}, dataRelayFlag{"a": liveRelay("a")}, []string{"a:3478"}, nil
	}, relaySets{})
	_ = dir.refresh()
	dir.DataRelays()["evil"] = liveRelay("evil")
	dir.STUN()[0] = "evil:1"
	if _, ok := dir.DataRelays()["evil"]; ok || dir.STUN()[0] != "a:3478" {
		t.Fatal("caller mutation reached the directory")
	}
}

func TestRelayDirectoryRunRefreshesUntilCancelled(t *testing.T) {
	calls := make(chan struct{}, 10)
	dir := newRelayDirectory(func() ([]rendezvous.Relay, dataRelayFlag, []string, error) {
		calls <- struct{}{}
		return []rendezvous.Relay{liveRelay("a")}, dataRelayFlag{}, nil, nil
	}, relaySets{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { dir.run(ctx, 5*time.Millisecond, io.Discard); close(done) }()
	for i := 0; i < 2; i++ {
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatal("run did not refresh")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop on cancel")
	}
}
