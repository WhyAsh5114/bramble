package rendezvous

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func ep(kind EndpointKind, addr string) Endpoint {
	return Endpoint{Kind: kind, Addr: netip.MustParseAddrPort(addr)}
}

func TestAnnouncementRoundTrip(t *testing.T) {
	in := Announcement{
		ep(KindSTUN, "203.0.113.7:51820"),
		ep(KindHost, "192.168.1.5:51820"),
		ep(KindSTUN, "[2001:db8::1]:51820"),
	}
	encoded, err := in.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	const want = "ep1:srflx=203.0.113.7:51820,host=192.168.1.5:51820,srflx=[2001:db8::1]:51820"
	if encoded != want {
		t.Fatalf("Encode = %q, want %q", encoded, want)
	}
	out, err := ParseAnnouncement(encoded)
	if err != nil {
		t.Fatalf("ParseAnnouncement: %v", err)
	}
	if fmt.Sprint(out) != fmt.Sprint(in) {
		t.Fatalf("round trip = %v, want %v", out, in)
	}
}

func TestEncodeCollapsesDuplicates(t *testing.T) {
	encoded, err := Announcement{ep(KindSTUN, "203.0.113.7:1"), ep(KindHost, "203.0.113.7:1")}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if encoded != "ep1:srflx=203.0.113.7:1" {
		t.Fatalf("got %q", encoded)
	}
}

func TestParseLegacyBareCandidate(t *testing.T) {
	got, err := ParseAnnouncement("203.0.113.9:51820")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindStatic || got[0].Addr.String() != "203.0.113.9:51820" {
		t.Fatalf("got %v", got)
	}
}

func TestParseEmptyMeansNoAddress(t *testing.T) {
	got, err := ParseAnnouncement("")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseSkipsUnknownKindButKeepsKnown(t *testing.T) {
	got, err := ParseAnnouncement("ep1:turn=198.51.100.1:3478,srflx=203.0.113.7:51820")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindSTUN {
		t.Fatalf("got %v", got)
	}
}

func TestParseRejectsMalformedAndUnusable(t *testing.T) {
	cases := map[string]string{
		"not an address":     "hello",
		"missing kind":       "ep1:203.0.113.7:51820",
		"bad address":        "ep1:srflx=203.0.113.999:51820",
		"port zero":          "ep1:srflx=203.0.113.7:0",
		"unspecified":        "ep1:srflx=0.0.0.0:51820",
		"multicast":          "ep1:srflx=224.0.0.1:51820",
		"link local":         "ep1:srflx=169.254.1.1:51820",
		"unspecified bare":   "[::]:51820",
		"zone":               "ep1:srflx=[2001:db8::1%eth0]:51820",
		"zone bare":          "[2001:db8::1%eth0]:51820",
		"zone with newline":  "ep1:srflx=[2001:db8::1%z]:1\npublic_key=ab\nallowed_ip=0.0.0.0/0\nendpoint=[2001:db8::2%q]:1",
		"zone newline bare":  "[2001:db8::1%z\nallowed_ip=0.0.0.0/0]:1",
		"unmapped unspec":    "ep1:srflx=[::ffff:0.0.0.0]:51820",
		"broadcast":          "ep1:srflx=255.255.255.255:51820",
		"broadcast bare":     "255.255.255.255:51820",
		"bad address, known": "ep1:turn=nope,srflx=203.0.113.7:51820",
	}
	for name, in := range cases {
		if _, err := ParseAnnouncement(in); err == nil {
			t.Errorf("%s: ParseAnnouncement(%q) succeeded, want an error", name, in)
		}
	}
}

func TestParseEnforcesSizeLimits(t *testing.T) {
	many := make([]string, MaxEndpoints+1)
	for i := range many {
		many[i] = fmt.Sprintf("srflx=203.0.113.7:%d", 1000+i)
	}
	if _, err := ParseAnnouncement("ep1:" + strings.Join(many, ",")); err == nil {
		t.Error("accepted more than MaxEndpoints entries")
	}
	if _, err := ParseAnnouncement("ep1:" + strings.Repeat("x", MaxAnnouncementLen)); err == nil {
		t.Error("accepted an over-long announcement")
	}
}

func TestEncodeRejectsBadInput(t *testing.T) {
	if _, err := (Announcement{}).Encode(); err == nil {
		t.Error("encoded an empty announcement")
	}
	if _, err := (Announcement{{Kind: "turn", Addr: netip.MustParseAddrPort("203.0.113.7:1")}}).Encode(); err == nil {
		t.Error("encoded an unknown kind")
	}
	if _, err := (Announcement{ep(KindSTUN, "0.0.0.0:1")}).Encode(); err == nil {
		t.Error("encoded an unspecified address")
	}
}

func TestBestPrefersStaticThenSTUNThenHost(t *testing.T) {
	a := Announcement{ep(KindHost, "192.168.1.5:1"), ep(KindSTUN, "203.0.113.7:2"), ep(KindStatic, "198.51.100.1:3")}
	if got, _ := a.Best(); got.String() != "198.51.100.1:3" {
		t.Errorf("Best = %s, want the static endpoint", got)
	}
	if got, _ := a[:2].Best(); got.String() != "203.0.113.7:2" {
		t.Errorf("Best = %s, want the STUN endpoint", got)
	}
	if _, ok := (Announcement{}).Best(); ok {
		t.Error("Best reported an endpoint for an empty announcement")
	}
}

// memRendezvous is an in-memory stand-in for the paid rendezvous channel:
// each Exchange publishes the caller's candidate for the peer and returns
// whatever the peer published for the caller. It records every call.
type memRendezvous struct {
	mu        sync.Mutex
	cond      *sync.Cond
	published map[[2]string]string // {from, to} -> candidate
	calls     [][2]string          // {own, peer}
}

func newMemRendezvous() *memRendezvous {
	r := &memRendezvous{published: map[[2]string]string{}}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *memRendezvous) exchange(own, peer, candidate string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, [2]string{own, peer})
	r.published[[2]string{own, peer}] = candidate
	r.cond.Broadcast()

	deadline := time.AfterFunc(2*time.Second, func() {
		r.mu.Lock()
		r.cond.Broadcast()
		r.mu.Unlock()
	})
	defer deadline.Stop()
	start := time.Now()
	for {
		if c, ok := r.published[[2]string{peer, own}]; ok {
			return c, nil
		}
		if time.Since(start) >= 2*time.Second {
			return "", fmt.Errorf("timed out waiting for %s", peer)
		}
		r.cond.Wait()
	}
}

func (r *memRendezvous) callsTo(peer string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c[1] == peer {
			n++
		}
	}
	return n
}

func admitting(keys ...string) func(string) bool {
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	return func(pk string) bool { return set[pk] }
}

func TestTwoNodesLearnEachOthersEndpoints(t *testing.T) {
	r := newMemRendezvous()
	aliceEPs := Announcement{ep(KindSTUN, "203.0.113.10:41000")}
	bobEPs := Announcement{ep(KindSTUN, "198.51.100.20:42000"), ep(KindHost, "10.0.0.2:51820")}

	alice := &Exchanger{Own: "alice", Local: func() Announcement { return aliceEPs }, Via: r.exchange, Admitted: admitting("bob")}
	bob := &Exchanger{Own: "bob", Local: func() Announcement { return bobEPs }, Via: r.exchange, Admitted: admitting("alice")}

	type result struct {
		got Announcement
		err error
	}
	aliceRes, bobRes := make(chan result, 1), make(chan result, 1)
	go func() { g, err := alice.Learn("bob"); aliceRes <- result{g, err} }()
	go func() { g, err := bob.Learn("alice"); bobRes <- result{g, err} }()

	a, b := <-aliceRes, <-bobRes
	if a.err != nil || b.err != nil {
		t.Fatalf("Learn errors: alice %v, bob %v", a.err, b.err)
	}
	if fmt.Sprint(a.got) != fmt.Sprint(bobEPs) {
		t.Errorf("alice learned %v, want %v", a.got, bobEPs)
	}
	if fmt.Sprint(b.got) != fmt.Sprint(aliceEPs) {
		t.Errorf("bob learned %v, want %v", b.got, aliceEPs)
	}
}

func TestLearnIgnoresNonAdmittedPeer(t *testing.T) {
	r := newMemRendezvous()
	// mallory is not in ENS and publishes an endpoint at alice anyway.
	r.published[[2]string{"mallory", "alice"}] = "ep1:srflx=203.0.113.66:6666"

	alice := &Exchanger{Own: "alice", Local: func() Announcement { return Announcement{ep(KindSTUN, "203.0.113.10:41000")} }, Via: r.exchange, Admitted: admitting("bob")}
	got, err := alice.Learn("mallory")
	if !errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("err = %v, want ErrNotAdmitted", err)
	}
	if len(got) != 0 {
		t.Fatalf("learned %v from a non-admitted peer", got)
	}
	if n := r.callsTo("mallory"); n != 0 {
		t.Fatalf("alice used the rendezvous channel %d time(s) for a non-admitted peer; her endpoints and payment leak", n)
	}
}

func TestLearnRefusesEveryoneWithoutAdmissionCheck(t *testing.T) {
	r := newMemRendezvous()
	r.published[[2]string{"bob", "alice"}] = "ep1:srflx=203.0.113.66:6666"
	alice := &Exchanger{Own: "alice", Via: r.exchange}
	if _, err := alice.Learn("bob"); !errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("err = %v, want ErrNotAdmitted when Admitted is nil", err)
	}
}

func TestLearnDiscardsEndpointsWhenPeerRevokedMidExchange(t *testing.T) {
	var mu sync.Mutex
	admitted := true
	via := func(own, peer, candidate string) (string, error) {
		mu.Lock()
		admitted = false // ENS revokes bob while the exchange waits
		mu.Unlock()
		return "ep1:srflx=203.0.113.66:6666", nil
	}
	alice := &Exchanger{Own: "alice", Via: via, Admitted: func(string) bool {
		mu.Lock()
		defer mu.Unlock()
		return admitted
	}}
	got, err := alice.Learn("bob")
	if !errors.Is(err, ErrNotAdmitted) || len(got) != 0 {
		t.Fatalf("got %v, %v; want no endpoints and ErrNotAdmitted", got, err)
	}
}

func TestLearnDropsLoopbackUnlessAllowed(t *testing.T) {
	via := func(string, string, string) (string, error) {
		return "ep1:srflx=127.0.0.1:51820,srflx=203.0.113.7:51820", nil
	}
	ex := &Exchanger{Own: "alice", Via: via, Admitted: admitting("bob")}
	got, err := ex.Learn("bob")
	if err != nil || len(got) != 1 || got[0].Addr.String() != "203.0.113.7:51820" {
		t.Fatalf("got %v, %v; want only the public endpoint", got, err)
	}
	ex.AllowLoopback = true
	if got, _ = ex.Learn("bob"); len(got) != 2 {
		t.Fatalf("AllowLoopback: got %v, want both endpoints", got)
	}
}

func TestLearnReportsUnusableAnnouncement(t *testing.T) {
	via := func(string, string, string) (string, error) { return "ep1:srflx=0.0.0.0:1", nil }
	ex := &Exchanger{Own: "alice", Via: via, Admitted: admitting("bob")}
	if _, err := ex.Learn("bob"); err == nil || errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("err = %v, want a parse error", err)
	}
}

func TestLearnPassesRendezvousFailureThrough(t *testing.T) {
	boom := errors.New("relay down")
	ex := &Exchanger{Own: "alice", Via: func(string, string, string) (string, error) { return "", boom }, Admitted: admitting("bob")}
	if _, err := ex.Learn("bob"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestLearnPublishesEmptyWhenNoLocalEndpoints(t *testing.T) {
	var published string
	via := func(_, _, c string) (string, error) { published = c; return "", nil }
	ex := &Exchanger{Own: "alice", Via: via, Admitted: admitting("bob")}
	got, err := ex.Learn("bob")
	if err != nil || len(got) != 0 || published != "" {
		t.Fatalf("got %v, %v, published %q", got, err, published)
	}
}
