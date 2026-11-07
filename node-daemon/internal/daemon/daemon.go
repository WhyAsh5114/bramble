package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Daemon is the running `bramble up`: one device, one state machine, at most
// one brambled child.
type Daemon struct {
	Store     *Store
	ConfigURL string
	Brambled  string
	TUN       bool
	Logger    *log.Logger

	// Intervals; zero means the default.
	PendingPoll  time.Duration // join status while pending (default 2 s)
	ApprovedPoll time.Duration // join status after approval (default 10 s)
	HealthPoll   time.Duration // node status API (default 1 s)
	// StartupGrace is how long a fresh node may stay silent before the UI
	// reports a problem. The sidecar waits for Tor first: up to 390 s
	// (brambled/sidecar.DefaultTorReadyTimeout).
	StartupGrace time.Duration
	// startNode is replaced in tests.
	startNode func(NodeSpec) (nodeHandle, error)

	mu sync.Mutex
	// joinEpoch counts Leave calls; joining is set while a join request is
	// in flight. Together they stop a slow request from changing the state
	// after the person has left, and stop two requests overlapping.
	joinEpoch uint64
	joining   bool
	m         *Machine
	id        Identity
	pubkey    string
	cfg       *NodeConfig
	node      nodeHandle
	nodeSince time.Time
	retryAt   time.Time
	backoff   time.Duration
	lastPoll  time.Time
	wake      chan struct{}
}

// nodeHandle is the part of *RunningNode the daemon uses.
type nodeHandle interface {
	Done() <-chan struct{}
	ExitSummary() string
	Stop(timeout time.Duration)
	FetchPeers(ctx context.Context, now time.Time) ([]Peer, error)
	ForwardPort(peer, service string) (int, bool)
}

// ForwardPort returns the loopback port forwarded to peer's service.
func (n *RunningNode) ForwardPort(peer, service string) (int, bool) {
	p, ok := n.Ports.Forwards[ServiceKey(peer, service)]
	return p, ok
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Logger != nil {
		d.Logger.Printf(format, args...)
	}
}

func durOr(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// Init loads (or creates) the identity and restores the state machine.
func (d *Daemon) Init(ctx context.Context) error {
	if err := d.Store.Ensure(); err != nil {
		return err
	}
	id, err := d.Store.LoadOrCreateIdentity()
	if err != nil {
		return err
	}
	pub, err := id.PublicKeyHex()
	if err != nil {
		return err
	}
	join, err := d.Store.LoadJoin()
	if err != nil {
		return err
	}
	d.id, d.pubkey = id, pub
	d.m = NewMachine(id.Name, join)
	d.wake = make(chan struct{}, 1)
	if d.startNode == nil {
		d.startNode = func(s NodeSpec) (nodeHandle, error) { return StartNode(s) }
	}
	if d.ConfigURL != "" {
		if cfg, err := FetchNodeConfig(ctx, d.ConfigURL); err == nil {
			d.cfg = cfg
		} else {
			d.logf("node config: %v", err)
		}
	}
	return nil
}

func (d *Daemon) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Run drives join polling and node supervision until ctx ends, then stops
// the node.
func (d *Daemon) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); d.pollLoop(ctx) }()
	go func() { defer wg.Done(); d.nodeLoop(ctx) }()
	wg.Wait()
	d.mu.Lock()
	node := d.node
	d.node = nil
	d.mu.Unlock()
	if node != nil {
		node.Stop(10 * time.Second)
	}
}

// State is GET /api/state's response (ETH-177 contract v1). PublicKey and
// DefaultTailnet are additions: the admin compares the key, and the UI can
// prefill the tailnet this install was set up for.
type State struct {
	Phase          Phase  `json:"phase"`
	Name           string `json:"name"`
	Fingerprint    string `json:"fingerprint"`
	PublicKey      string `json:"publicKey"`
	Tailnet        string `json:"tailnet,omitempty"`
	DefaultTailnet string `json:"defaultTailnet,omitempty"`
	JoinID         string `json:"joinId,omitempty"`
	MeshIP         string `json:"meshIp,omitempty"`
	IsAdmin        bool   `json:"isAdmin"`
	Peers          []Peer `json:"peers"`
	// Services lists the tailnet services this device can open in a
	// browser, once it is approved. Each URL is its loopback forward.
	Services []ServiceLink `json:"services,omitempty"`
	Error    *ErrorInfo    `json:"error,omitempty"`
}

// ServiceLink is one service from the node config and the loopback URL
// that reaches it. A link only says where the forward listens; the gateway
// still decides each connection.
type ServiceLink struct {
	Peer    string `json:"peer"`
	Service string `json:"service"`
	URL     string `json:"url"`
}

// Fingerprint is the first 16 hex digits of the public key in groups of
// four, so a person can compare it with the key the admin sees.
func Fingerprint(pubkeyHex string) string {
	s := pubkeyHex
	if len(s) > 16 {
		s = s[:16]
	}
	var groups []string
	for len(s) > 4 {
		groups = append(groups, s[:4])
		s = s[4:]
	}
	return strings.Join(append(groups, s), " ")
}

// State returns the current state.
func (d *Daemon) State() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := State{
		Phase:       d.m.Phase(),
		Name:        d.m.Name(),
		Fingerprint: Fingerprint(d.pubkey),
		PublicKey:   d.pubkey,
		Tailnet:     d.m.Tailnet(),
		JoinID:      d.m.JoinID(),
		MeshIP:      d.m.MeshIP(),
		IsAdmin:     d.Store.AdminToken() != "",
		Peers:       d.m.Peers(),
		Error:       d.m.Reason(),
	}
	if d.cfg != nil {
		st.DefaultTailnet = d.cfg.Tailnet
	}
	if base := d.m.Base(); (base == PhaseConnected || base == PhaseApproved) && d.node != nil && d.cfg != nil {
		for _, s := range d.cfg.Services {
			if s.Peer == st.Name {
				continue
			}
			if port, ok := d.node.ForwardPort(s.Peer, s.Service); ok {
				st.Services = append(st.Services, ServiceLink{Peer: s.Peer, Service: s.Service, URL: fmt.Sprintf("http://127.0.0.1:%d%s", port, s.Path)})
			}
		}
	}
	return st
}

// Setup sets the device name.
func (d *Daemon) Setup(name string) error {
	name = strings.TrimSpace(name)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.m.Setup(name); err != nil {
		return err
	}
	d.id.Name = name
	if err := d.Store.SaveIdentity(d.id); err != nil {
		return &ErrorInfo{Message: "Bramble could not save the device name: " + err.Error(), NextStep: "Check that " + d.Store.Dir + " is writable, then try again."}
	}
	return nil
}

// config returns the node config, fetching it if needed.
func (d *Daemon) config(ctx context.Context, refresh bool) (*NodeConfig, error) {
	d.mu.Lock()
	cfg, src := d.cfg, d.ConfigURL
	d.mu.Unlock()
	if cfg != nil && !refresh {
		return cfg, nil
	}
	if src == "" {
		return nil, &ErrorInfo{
			Message:  "Bramble does not know where to find the tailnet's settings.",
			NextStep: "Restart it with `bramble up -config https://<install host>`, using the host you installed from.",
		}
	}
	fresh, err := FetchNodeConfig(ctx, src)
	if err != nil {
		if cfg != nil {
			d.logf("node config refresh failed, keeping the last one: %v", err)
			return cfg, nil
		}
		return nil, &ErrorInfo{
			Message:  "Bramble could not load the tailnet's settings from " + src + ".",
			NextStep: "Check the internet connection and the install host, then try again.",
		}
	}
	d.mu.Lock()
	d.cfg = fresh
	d.mu.Unlock()
	return fresh, nil
}

// errJoinCancelled is returned to a Join whose request was overtaken by Leave.
var errJoinCancelled = &ErrorInfo{
	Message:  "The join request was cancelled because this device left the tailnet.",
	NextStep: "Press Join again if you still want to join.",
}

// Join sends a join request for tailnet.
func (d *Daemon) Join(ctx context.Context, tailnet string) error {
	tailnet = strings.ToLower(strings.TrimSpace(tailnet))
	d.mu.Lock()
	err := d.m.CanJoin()
	if err == nil && d.joining {
		err = busy("A join request is already being sent.", "Wait a moment, then check the status.")
	}
	name, epoch := d.m.Name(), d.joinEpoch
	if err == nil {
		d.joining = true
	}
	d.mu.Unlock()
	if err != nil {
		return err
	}
	defer func() {
		d.mu.Lock()
		d.joining = false
		d.mu.Unlock()
	}()
	cfg, err := d.config(ctx, true)
	if err != nil {
		return d.failIf(epoch, err)
	}
	if tailnet == "" {
		tailnet = cfg.Tailnet
	}
	if tailnet != cfg.Tailnet {
		return &ErrorInfo{
			Message:  fmt.Sprintf("This install is set up for %s, not %s.", cfg.Tailnet, tailnet),
			NextStep: fmt.Sprintf("Check the spelling, or install Bramble from the host that runs %s.", tailnet),
		}
	}
	jc := &JoinClient{BaseURL: cfg.JoinAPI}
	j, err := jc.Request(ctx, name, d.pubkey, tailnet)
	if err != nil {
		var apiErr *JoinAPIError
		switch {
		case errors.As(err, &apiErr) && apiErr.Status == 429:
			return d.failIf(epoch, &ErrorInfo{Message: "The approval service is getting too many requests.", NextStep: "Wait a minute, then press Join again."})
		case errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500:
			return d.failIf(epoch, &ErrorInfo{Message: "The approval service refused the request: " + apiErr.Message, NextStep: "Check the device name, or ask the tailnet admin."})
		default:
			return d.failIf(epoch, &ErrorInfo{Message: "Bramble could not reach the approval service at " + cfg.JoinAPI + ".", NextStep: "Check the internet connection, then press Join again."})
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.joinEpoch != epoch {
		// Leave ran while the request was in flight. The approval service
		// may still hold a pending request; the admin can deny it.
		return errJoinCancelled
	}
	if err := d.Store.SaveJoin(JoinRecord{Tailnet: tailnet, JoinID: j.ID}); err != nil {
		d.logf("saving join id: %v", err)
	}
	d.m.JoinSent(tailnet, j.ID)
	d.m.JoinUpdate(*j)
	d.lastPoll = time.Now()
	d.poke()
	return nil
}

// failIf records err as the current problem, unless Leave has run since
// epoch (the failure then belongs to a request nobody is waiting on), and
// returns it.
func (d *Daemon) failIf(epoch uint64, err error) error {
	var info *ErrorInfo
	if errors.As(err, &info) {
		d.mu.Lock()
		if d.joinEpoch == epoch {
			d.m.SetProblem(*info)
		}
		d.mu.Unlock()
	}
	return err
}

// fail records err as the current problem and returns it.
func (d *Daemon) fail(err error) error {
	var info *ErrorInfo
	if errors.As(err, &info) {
		d.mu.Lock()
		d.m.SetProblem(*info)
		d.mu.Unlock()
	}
	return err
}

// Leave stops the node and forgets the tailnet.
func (d *Daemon) Leave() {
	d.mu.Lock()
	d.joinEpoch++
	d.m.Leave()
	node := d.node
	d.node = nil
	if err := d.Store.ClearJoin(); err != nil {
		d.logf("forgetting join id: %v", err)
	}
	d.mu.Unlock()
	if node != nil {
		node.Stop(10 * time.Second)
	}
}

func (d *Daemon) pollLoop(ctx context.Context) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		d.mu.Lock()
		want, id, base, last := d.m.WantsPolling(), d.m.JoinID(), d.m.Base(), d.lastPoll
		d.mu.Unlock()
		if !want {
			continue
		}
		every := durOr(d.PendingPoll, 2*time.Second)
		if base != PhasePending {
			every = durOr(d.ApprovedPoll, 10*time.Second)
		}
		if time.Since(last) < every {
			continue
		}
		cfg, err := d.config(ctx, false)
		if err != nil {
			d.fail(err)
			d.mu.Lock()
			d.lastPoll = time.Now()
			d.mu.Unlock()
			continue
		}
		j, err := (&JoinClient{BaseURL: cfg.JoinAPI}).Get(ctx, id)
		d.mu.Lock()
		d.lastPoll = time.Now()
		switch {
		case d.m.JoinID() != id:
			// Left, or joined again, while the request was in flight.
		case errors.Is(err, ErrJoinNotFound):
			d.m.JoinLost()
		case err != nil:
			if d.m.Base() == PhasePending {
				d.m.SetProblem(ErrorInfo{
					Message:  "Bramble cannot reach the approval service to check on the request.",
					NextStep: "Check the internet connection. Bramble keeps trying on its own.",
				})
			} else {
				d.logf("join status: %v", err)
			}
		default:
			d.m.JoinUpdate(*j)
		}
		if d.m.JoinID() == "" {
			if err := d.Store.ClearJoin(); err != nil {
				d.logf("forgetting join id: %v", err)
			}
		}
		d.mu.Unlock()
		d.poke()
	}
}

func (d *Daemon) nodeLoop(ctx context.Context) {
	tick := time.NewTicker(durOr(d.HealthPoll, time.Second))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-d.wake:
		}
		d.reconcileNode(ctx)
	}
}

func (d *Daemon) reconcileNode(ctx context.Context) {
	d.mu.Lock()
	want, node := d.m.WantsNode(), d.node
	d.mu.Unlock()

	switch {
	case !want && node != nil:
		d.mu.Lock()
		d.node = nil
		d.mu.Unlock()
		node.Stop(10 * time.Second)
	case want && node == nil:
		d.mu.Lock()
		wait := time.Now().Before(d.retryAt)
		d.mu.Unlock()
		if !wait {
			d.launch(ctx)
		}
	case want && node != nil:
		select {
		case <-node.Done():
			d.nodeFailed(ErrorInfo{
				Message:  "The connection stopped: " + node.ExitSummary(),
				NextStep: "Bramble restarts it on its own. If this keeps happening, run `bramble down`, then `bramble up`, and tell the tailnet admin.",
			})
			return
		default:
		}
		peers, err := node.FetchPeers(ctx, time.Now())
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.node != node {
			return
		}
		if err == nil {
			d.backoff = 0
			d.m.PeersUpdate(peers)
			return
		}
		if time.Since(d.nodeSince) > durOr(d.StartupGrace, 8*time.Minute) {
			d.m.NodeDown(ErrorInfo{
				Message:  "The connection is not answering.",
				NextStep: "Wait a minute. If it stays like this, run `bramble down` and `bramble up` again.",
			})
		}
	}
}

func (d *Daemon) nodeFailed(info ErrorInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.node = nil
	d.m.NodeDown(info)
	d.backoff = min(max(2*d.backoff, 5*time.Second), time.Minute)
	d.retryAt = time.Now().Add(d.backoff)
	d.logf("node stopped: %s (retry in %s)", info.Message, d.backoff)
}

func (d *Daemon) launch(ctx context.Context) {
	cfg, err := d.config(ctx, true)
	if err != nil {
		var info *ErrorInfo
		if errors.As(err, &info) {
			d.nodeFailed(*info)
		}
		return
	}
	d.mu.Lock()
	spec := NodeSpec{Brambled: d.Brambled, Label: d.id.Name, PrivateKeyHex: d.id.PrivateKeyHex, Config: cfg, TUN: d.TUN}
	d.mu.Unlock()
	node, err := d.startNode(spec)
	if err != nil {
		d.nodeFailed(ErrorInfo{Message: "Bramble could not start the connection: " + err.Error(), NextStep: "Reinstall Bramble, then run `bramble up` again."})
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.m.WantsNode() {
		go node.Stop(10 * time.Second)
		return
	}
	d.node = node
	d.nodeSince = time.Now()
	d.logf("node started for %s on %s", spec.Label, cfg.Tailnet)
}

// TestResult is POST /api/test's response.
type TestResult struct {
	OK      bool       `json:"ok"`
	Status  int        `json:"status"`
	MS      int64      `json:"ms"`
	Peer    string     `json:"peer,omitempty"`
	Service string     `json:"service,omitempty"`
	Error   *ErrorInfo `json:"error,omitempty"`
}

// testTimeout covers the gateway's ACL decision: it reads three ENS records
// over Tor before it answers (brambled/gateway gatewayDecisionDeadline is
// 90 s). A first request took 21.8 s on 6 Nov 2026 (Verified, dev tailnet).
const testTimeout = 95 * time.Second

// testTarget picks the configured service matching peer and service; empty
// values match anything.
func testTarget(cfg *NodeConfig, own, peer, service string) (Service, bool) {
	if cfg == nil {
		return Service{}, false
	}
	for _, s := range cfg.Services {
		if s.Peer == own {
			continue
		}
		if (peer == "" || s.Peer == peer) && (service == "" || s.Service == service) {
			return s, true
		}
	}
	return Service{}, false
}

// Test fetches a tailnet service through the mesh via its loopback forward.
// RequestAccess asks the operator for one configured service.
func (d *Daemon) RequestAccess(ctx context.Context, peer, service string) error {
	d.mu.Lock()
	own, phase := d.m.Name(), d.m.Base()
	d.mu.Unlock()
	if phase != PhaseConnected && phase != PhaseApproved {
		return &ErrorInfo{Message: "This device is not approved yet.", NextStep: "Wait for the owner to approve it, then try again."}
	}
	cfg, err := d.config(ctx, true)
	if err != nil {
		return err
	}
	for _, s := range cfg.Services {
		if s.Peer == peer && s.Service == service {
			if err := (&JoinClient{BaseURL: cfg.JoinAPI}).RequestAccess(ctx, own, peer, service); err != nil {
				return &ErrorInfo{Message: "The access request did not go through.", NextStep: "Check your connection, then try again."}
			}
			return nil
		}
	}
	return &ErrorInfo{Message: "This network does not offer that service.", NextStep: "Reload this page and try again."}
}

func (d *Daemon) Test(ctx context.Context, peer, service string, get func(ctx context.Context, url string) (int, error)) TestResult {
	d.mu.Lock()
	node, cfg, own, phase := d.node, d.cfg, d.m.Name(), d.m.Base()
	d.mu.Unlock()
	if phase != PhaseConnected && phase != PhaseApproved || node == nil {
		return TestResult{Error: &ErrorInfo{Message: "This device is not connected yet.", NextStep: "Join a tailnet and wait until it shows Connected."}}
	}
	target, ok := testTarget(cfg, own, peer, service)
	if !ok {
		return TestResult{Peer: peer, Service: service, Error: &ErrorInfo{Message: "This tailnet offers no test service by that name.", NextStep: "Ask the tailnet admin which service to test."}}
	}
	port, ok := node.ForwardPort(target.Peer, target.Service)
	if !ok {
		return TestResult{Peer: target.Peer, Service: target.Service, Error: &ErrorInfo{Message: "The test service was added after the connection started.", NextStep: "Leave and join again, or restart Bramble."}}
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	status, err := get(ctx, fmt.Sprintf("http://127.0.0.1:%d%s", port, target.Path))
	res := TestResult{Status: status, MS: time.Since(start).Milliseconds(), Peer: target.Peer, Service: target.Service}
	switch {
	case err != nil:
		res.Error = &ErrorInfo{
			Message:  fmt.Sprintf("%s did not answer for %s.", target.Peer, target.Service),
			NextStep: fmt.Sprintf("If the admin has not granted this device access to %s yet, ask for it. Otherwise wait until %s shows a path and try again.", target.Service, target.Peer),
		}
	case status >= 200 && status < 300:
		res.OK = true
	default:
		res.Error = &ErrorInfo{Message: fmt.Sprintf("%s answered with HTTP %d.", target.Service, status), NextStep: "Ask the tailnet admin to check the service."}
	}
	return res
}
