// Package statusapi is a loopback-only HTTP surface exposing this node's
// own runtime state — live WireGuard peer connectivity and a recent feed of
// admission/gateway decisions — to local tooling (the dashboard, via its
// own server-side proxy routes). It never talks to ENS or Hedera itself and
// never accepts a write that changes admission, ACL, or payment state; the
// one action it exposes (ping) only ever probes a peer brambled is already
// tracking, over the tunnel it already owns, and reports the outcome — a
// diagnostic, not a capability grant. See docs/ for why the dashboard stays
// read-only for everything else.
package statusapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/admission"
	"github.com/WhyAsh5114/bramble/brambled/gateway"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// PeerTable is satisfied by *wgnode.Node — the live WireGuard peer table,
// keyed by pubkey. admission.Event (below) is what maps a pubkey back to
// the ENS label a human/demo cares about; joining the two is this
// package's whole job.
type PeerTable interface {
	Peers() ([]wgnode.PeerInfo, error)
}

// ActivityEvent is one entry in the activity feed — an admission decision,
// a gateway CONNECT decision, or a peer dropped from the WireGuard peer
// table, whichever produced it.
type ActivityEvent struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"` // "admission", "gateway" or "peer"
	Label   string    `json:"label"`
	Message string    `json:"message"`
	// Outcome is "allowed" or "denied" for a gateway event and "dropped" for
	// a peer event. Admission events leave it empty. Consumers read this
	// field instead of parsing Message.
	Outcome string `json:"outcome,omitempty"`
	// Service is the gateway's symbolic service name. Only gateway events set it.
	Service string `json:"service,omitempty"`
}

const activityBufferSize = 200

// activityBuffer is a small fixed-size ring buffer, safe for concurrent
// writes from the admission/gateway callback goroutines and concurrent
// reads from HTTP handler goroutines.
type activityBuffer struct {
	mu     sync.Mutex
	events []ActivityEvent // oldest first, trimmed to activityBufferSize
}

func (b *activityBuffer) add(e ActivityEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	if len(b.events) > activityBufferSize {
		b.events = b.events[len(b.events)-activityBufferSize:]
	}
}

func (b *activityBuffer) snapshot() []ActivityEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ActivityEvent, len(b.events))
	copy(out, b.events)
	return out
}

// admissionCache holds the most recent admission.Event per label — the
// label->pubkey/authorized mapping admission.Loop already computes on every
// sync pass but doesn't otherwise expose outside its own OnEvent callback.
type admissionCache struct {
	mu      sync.Mutex
	byLabel map[string]admission.Event
	// lastNote is the message last recorded to the activity feed per label.
	// admission.Loop reports every peer on every sync pass; the feed records
	// only changes so repeated passes do not push real events out of the ring.
	lastNote map[string]string
	// lastAuthorized is the last authorization state reached without an error.
	// An error event says nothing about whether the peer is still admitted.
	lastAuthorized map[string]bool
}

// set stores e and reports whether it changed the feed-visible state of its
// label, and whether it moved a previously authorized peer out of the table.
func (c *admissionCache) set(e admission.Event, note string) (changed, dropped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byLabel == nil {
		c.byLabel = map[string]admission.Event{}
		c.lastNote = map[string]string{}
		c.lastAuthorized = map[string]bool{}
	}
	c.byLabel[e.Label] = e
	prevNote, seen := c.lastNote[e.Label]
	changed = !seen || prevNote != note
	c.lastNote[e.Label] = note
	if e.Err == nil {
		dropped = c.lastAuthorized[e.Label] && !e.Authorized
		c.lastAuthorized[e.Label] = e.Authorized
	}
	return changed, dropped
}

func (c *admissionCache) get(label string) (admission.Event, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byLabel[label]
	return e, ok
}

func (c *admissionCache) snapshot() map[string]admission.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]admission.Event, len(c.byLabel))
	for k, v := range c.byLabel {
		out[k] = v
	}
	return out
}

// Server serves this node's own runtime state. Zero value is usable once
// PeerTable is set.
type Server struct {
	PeerTable PeerTable
	// EndpointSource supplies the node's in-memory STUN observation.
	EndpointSource interface{ EndpointStatus() wgnode.EndpointStatus }
	// DataRelays is the data-relay pool this node uses, label to relay
	// sidecar URL, from -data-relay flags and ENS discovery alike. A caller
	// that sees a peer's WireGuard endpoint on one of these hosts knows the
	// path is relayed. It is called on every request, because discovery can
	// add relays while the node runs.
	DataRelays func() map[string]string

	admission admissionCache
	activity  activityBuffer
	paths     pathCache
}

// OnAdmissionEvent is meant to be assigned directly to admission.Loop.OnEvent.
func (s *Server) OnAdmissionEvent(e admission.Event) {
	msg := fmt.Sprintf("authorized=%v", e.Authorized)
	if e.Err != nil {
		msg = fmt.Sprintf("resolve error: %v", e.Err)
	}
	changed, dropped := s.admission.set(e, msg)
	now := time.Now()
	if changed {
		s.activity.add(ActivityEvent{Time: now, Kind: "admission", Label: e.Label, Message: msg})
	}
	if dropped {
		s.activity.add(ActivityEvent{
			Time: now, Kind: "peer", Label: e.Label, Outcome: "dropped",
			Message: "dropped from the WireGuard peer table: no longer authorized",
		})
	}
}

// OnGatewayDecision is meant to be assigned directly to gateway.Server.OnDecision.
func (s *Server) OnGatewayDecision(d gateway.Decision) {
	verb := "denied"
	if d.Allowed {
		verb = "allowed"
	}
	msg := fmt.Sprintf("%s %q: %s (%s)", verb, d.Service, d.Detail, d.RequesterLabel)
	s.activity.add(ActivityEvent{
		Time: time.Now(), Kind: "gateway", Label: d.RequesterLabel, Message: msg,
		Outcome: verb, Service: d.Service,
	})
}

// PeerStatus is one row of GET /peers.
type PeerStatus struct {
	Label               string   `json:"label"`
	PublicKeyHex        string   `json:"publicKeyHex"`
	Authorized          bool     `json:"authorized"`
	AllowedIPs          []string `json:"allowedIPs"`
	Handshaked          bool     `json:"handshaked"`
	LastHandshakeUnixNs int64    `json:"lastHandshakeUnixNs"`
	RxBytes             int64    `json:"rxBytes"`
	// Endpoint is the peer's current WireGuard UDP endpoint, if known.
	Endpoint       string `json:"endpoint,omitempty"`
	Path           string `json:"path"`
	TraversalState string `json:"traversalState,omitempty"`
}

func (s *Server) wgPeersByPubkey() (map[string]wgnode.PeerInfo, error) {
	byPubkey := map[string]wgnode.PeerInfo{}
	if s.PeerTable == nil {
		return byPubkey, nil
	}
	peers, err := s.PeerTable.Peers()
	if err != nil {
		return nil, err
	}
	for _, p := range peers {
		byPubkey[p.PublicKeyHex] = p
	}
	return byPubkey, nil
}

func (s *Server) handlePeers(w http.ResponseWriter, _ *http.Request) {
	out, err := s.Peers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, out)
}

// Peers returns every tracked peer with its admission, WireGuard and path state.
func (s *Server) Peers() ([]PeerStatus, error) {
	byPubkey, err := s.wgPeersByPubkey()
	if err != nil {
		return nil, err
	}

	admitted := s.admission.snapshot()
	out := make([]PeerStatus, 0, len(admitted))
	for label, ev := range admitted {
		ps := PeerStatus{Label: label, PublicKeyHex: ev.PublicKey, Authorized: ev.Authorized, Path: "unknown"}
		if wp, ok := byPubkey[ev.PublicKey]; ok {
			ps.AllowedIPs = wp.AllowedIPs
			ps.Handshaked = wp.Handshaked()
			ps.LastHandshakeUnixNs = wp.LastHandshakeUnixNs
			ps.RxBytes = wp.RxBytes
			ps.Endpoint = wp.Endpoint
			if ev.Authorized {
				ps.Path, ps.TraversalState = s.path(label, ev.PublicKey)
			}
		}
		out = append(out, ps)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// Activity returns recent admission and gateway events, newest last.
func (s *Server) Activity() []ActivityEvent { return s.activity.snapshot() }

func (s *Server) handleActivity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.activity.snapshot())
}

type pingRequest struct {
	Label string `json:"label"`
}

type pingResponse struct {
	Reachable bool   `json:"reachable"`
	RTT       string `json:"rtt,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// pingRTTPattern matches both BSD/macOS ("time=1.234 ms") and iputils/Linux
// ("time=1.23 ms") ping output — both use "time=", differing only in
// decimal precision, which this doesn't need to care about.
var pingRTTPattern = regexp.MustCompile(`time[=<]([0-9.]+) ?ms`)

// handlePing probes a single already-tracked peer over the real tunnel. The
// label must already be one brambled has emitted an admission event for
// (rejecting anything else with 400) — the caller supplies a label, never
// an address, so this can't be turned into an arbitrary-host prober.
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req pingRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.Label == "" {
		http.Error(w, "label required", http.StatusBadRequest)
		return
	}

	ev, ok := s.admission.get(req.Label)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown peer label %q", req.Label), http.StatusBadRequest)
		return
	}

	byPubkey, err := s.wgPeersByPubkey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	wp, ok := byPubkey[ev.PublicKey]
	if !ok || len(wp.AllowedIPs) == 0 {
		writeJSON(w, pingResponse{Reachable: false, Detail: "peer not currently in the WireGuard peer table"})
		return
	}
	prefix, err := netip.ParsePrefix(wp.AllowedIPs[0])
	if err != nil {
		writeJSON(w, pingResponse{Reachable: false, Detail: "peer's allowed-ip is unparseable: " + err.Error()})
		return
	}

	reachable, rtt := runPing(prefix.Addr())
	writeJSON(w, pingResponse{Reachable: reachable, RTT: rtt})
}

// runPing shells out to the OS's own ping binary, one echo, ~1s timeout —
// the documented way to verify connectivity on a real OS TUN node
// (wgnode.Node.Ping is netstack-only, see its doc comment). Flag names
// differ between BSD/macOS ping and iputils/Linux ping for the same
// "wait at most N seconds" behavior.
func runPing(addr netip.Addr) (bool, string) {
	args := []string{"-c", "1"}
	if runtime.GOOS == "darwin" {
		args = append(args, "-t", "1")
	} else {
		args = append(args, "-W", "1")
	}
	args = append(args, addr.String())

	out, err := exec.Command("ping", args...).CombinedOutput()
	if err != nil {
		return false, ""
	}
	if m := pingRTTPattern.FindStringSubmatch(string(out)); len(m) == 2 {
		return true, m[1] + "ms"
	}
	return true, ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Handler returns the mux mounting /peers, /activity, /ping, /endpoint, /relays.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /endpoint", func(w http.ResponseWriter, _ *http.Request) {
		state := wgnode.EndpointStatus{State: "disabled"}
		if s.EndpointSource != nil {
			state = s.EndpointSource.EndpointStatus()
		}
		writeJSON(w, state)
	})
	mux.HandleFunc("GET /relays", func(w http.ResponseWriter, _ *http.Request) {
		pool := map[string]string{}
		if s.DataRelays != nil {
			pool = s.DataRelays()
		}
		writeJSON(w, map[string]map[string]string{"dataRelays": pool})
	})
	mux.HandleFunc("/peers", s.handlePeers)
	mux.HandleFunc("/activity", s.handleActivity)
	mux.HandleFunc("/ping", s.handlePing)
	return mux
}

// ListenAndServe binds bindAddr:port and serves until ctx is cancelled.
// bindAddr defaults to "127.0.0.1" (see -status-bind in brambled/main.go) —
// loopback-only, same discipline as sidecar/src/index.ts's ENS sidecar,
// since this is one node's own runtime state that normally has no reason to
// be reachable from the network. A caller that genuinely needs a remote
// dashboard to reach this node (e.g. the node itself lives on a different
// machine than the browser) can opt into 0.0.0.0 explicitly; the ping
// action stays safe even then, since it only ever probes a label this node
// already tracks, never a caller-supplied address.
func (s *Server) ListenAndServe(ctx context.Context, bindAddr string, port uint) error {
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", bindAddr, port), Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
