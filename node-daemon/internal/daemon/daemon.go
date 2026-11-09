// Package daemon is the bramble background process: it keeps this device's
// key, watches its tailnet on-chain through a local registry cache, runs the
// node once the device is enrolled, and serves the local web UI's API.
//
// Nothing here talks to a Bramble server. Creating a tailnet, enrolling a
// device and granting access are wallet transactions the UI sends from the
// browser; the daemon only reads the chain and computes values that need this
// device's key (ADR 0019).
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled"
	"github.com/WhyAsh5114/bramble/brambled/gateway"
	"github.com/WhyAsh5114/bramble/brambled/registry"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/statusapi"
)

// Phases of this device.
const (
	PhaseSetup   = "setup"   // no tailnet chosen
	PhaseSyncing = "syncing" // reading the tailnet from the chain for the first time
	PhaseWaiting = "waiting" // chosen, but this device is not enrolled with its key yet
	PhaseMember  = "member"  // enrolled; the node runs
	PhaseRevoked = "revoked" // the tailnet revoked this device or its record expired
)

// Daemon is the background process. Fields above mu are set before Run.
type Daemon struct {
	Store      *Store
	Network    registry.Network
	Logger     *log.Logger
	Version    string
	ListenPort uint16 // WireGuard UDP port; 0 picks 41641
	TUN        bool   // real interface instead of userspace networking
	// RPCOverride replaces every RPC endpoint (tests).
	RPCOverride []string

	mu       sync.Mutex
	id       Identity
	pub      string
	saved    Saved
	rpc      *registry.RPC
	tn       *registry.Tailnet
	tnCancel context.CancelFunc
	tnRef    string
	node     *nodeRun
	phase    string
	problem  string
	ping     *pingServer
	forwards map[string]*forward
	wake     chan struct{}
	relayMu  sync.Mutex
	relayAt  time.Time
	relayOut []RelayInfo
}

type nodeRun struct {
	key    string
	cancel context.CancelFunc
	done   chan struct{}
	handle *brambled.Handle
	err    error
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Logger != nil {
		d.Logger.Printf(format, args...)
	}
}

// Init loads the key and saved state.
func (d *Daemon) Init() error {
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
	saved, err := d.Store.LoadSaved()
	if err != nil {
		return err
	}
	if _, err := rendezvous.RegisterIdentity(id.PrivateKeyHex); err != nil {
		return err
	}
	if d.Network.ChainID == 0 {
		d.Network = registry.Sepolia
	}
	d.id, d.pub, d.saved = id, pub, saved
	d.rpc = d.newRPC(saved.RPCURLs)
	d.forwards = map[string]*forward{}
	d.wake = make(chan struct{}, 1)
	d.phase = PhaseSetup
	return nil
}

// newRPC puts the user's endpoints first and the public ones after.
func (d *Daemon) newRPC(user []string) *registry.RPC {
	if len(d.RPCOverride) > 0 {
		return registry.NewRPC(d.RPCOverride)
	}
	return registry.NewRPC(append(append([]string(nil), user...), registry.DefaultRPCURLs...))
}

func (d *Daemon) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Run keeps the device in step with the chain until ctx ends.
func (d *Daemon) Run(ctx context.Context) {
	ping, err := startPing(d)
	if err != nil {
		d.logf("ping service: %v", err)
	}
	d.mu.Lock()
	d.ping = ping
	d.mu.Unlock()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		d.reconcile(ctx)
		select {
		case <-ctx.Done():
			d.mu.Lock()
			d.stopNodeLocked()
			d.stopCacheLocked()
			for _, f := range d.forwards {
				f.ln.Close()
			}
			d.mu.Unlock()
			if ping != nil {
				ping.ln.Close()
			}
			return
		case <-t.C:
		case <-d.wake:
		}
	}
}

func (d *Daemon) stopCacheLocked() {
	if d.tnCancel != nil {
		d.tnCancel()
	}
	d.tn, d.tnCancel, d.tnRef = nil, nil, ""
}

func (d *Daemon) stopNodeLocked() {
	if d.node == nil {
		return
	}
	n := d.node
	d.node = nil
	n.cancel()
	d.mu.Unlock()
	select {
	case <-n.done:
	case <-time.After(5 * time.Second):
	}
	d.mu.Lock()
}

func (d *Daemon) reconcile(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ref := d.saved.Tailnet
	if ref == nil {
		d.stopNodeLocked()
		d.stopCacheLocked()
		d.phase, d.problem = PhaseSetup, ""
		return
	}
	key := fmt.Sprintf("%s@%s#%d", ref.Name, ref.Registry, ref.Start)
	if d.tn == nil || d.tnRef != key {
		d.stopNodeLocked()
		d.stopCacheLocked()
		tn, err := registry.NewTailnet(registry.TailnetConfig{
			RPC: d.rpc, Name: ref.Name, Registry: ref.Registry, RegistryStart: ref.Start, Resolver: ref.Resolver,
			RelayRegistry: d.Network.RelayRegistry, RelayRegistryStart: d.Network.RelayStart, RelaySuffix: d.Network.RelaySuffix,
			Path: d.Store.CachePath(),
		})
		if err != nil {
			d.phase, d.problem = PhaseSyncing, err.Error()
			return
		}
		cctx, cancel := context.WithCancel(ctx)
		d.tn, d.tnCancel, d.tnRef = tn, cancel, key
		go tn.Run(cctx)
	}
	st := d.tn.Status()
	if st.LastSync.IsZero() {
		d.phase = PhaseSyncing
		d.problem = st.Error
		return
	}
	rec, err := d.tn.ResolveDevice(d.saved.Label)
	switch {
	case err != nil || rec.Pubkey == nil || *rec.Pubkey != d.pub:
		d.stopNodeLocked()
		d.phase, d.problem = PhaseWaiting, ""
		if err == nil && rec.Pubkey != nil && *rec.Pubkey != d.pub {
			d.problem = fmt.Sprintf("%s is enrolled with a different key. Pick another device name, or ask the admin to replace it.", d.saved.Label)
		}
		return
	case rec.Revoked || expired(rec.Expiry):
		d.stopNodeLocked()
		d.phase, d.problem = PhaseRevoked, ""
		return
	}
	d.phase = PhaseMember
	d.ensureNodeLocked(ctx)
}

func expired(expiry string) bool {
	n, err := strconv.ParseInt(expiry, 10, 64)
	return err != nil || n <= time.Now().Unix()
}

// nodeArgs builds brambled serve flags. The private key stays in-process.
func (d *Daemon) nodeArgsLocked() []string {
	port := d.ListenPort
	if port == 0 {
		port = 41641
	}
	args := []string{"-label", d.saved.Label, "-private-key", d.id.PrivateKeyHex, "-listen-port", strconv.Itoa(int(port)),
		"-status-port", "0", "-ttl", "5s", "-max-stale", "2m"}
	if !d.TUN {
		args = append(args, "-netstack")
	}
	if d.ping != nil && !d.saved.NoPing {
		args = append(args, "-service", fmt.Sprintf("%s=%d/open", PingService, d.ping.port))
	}
	for _, s := range d.saved.Services {
		v := fmt.Sprintf("%s=%d", s.Name, s.Port)
		if s.Open {
			v += "/open"
		}
		args = append(args, "-service", v)
	}
	if d.saved.RelayFirst {
		args = append(args, "-relay-first")
	}
	return args
}

func (d *Daemon) ensureNodeLocked(ctx context.Context) {
	args := d.nodeArgsLocked()
	key := strings.Join(args, "\x00")
	if d.node != nil && d.node.key == key {
		select {
		case <-d.node.done:
			// It exited: restart below after recording why.
			d.problem = fmt.Sprintf("the node stopped: %v; restarting", d.node.err)
			d.node = nil
		default:
			return
		}
	}
	d.stopNodeLocked()
	nctx, cancel := context.WithCancel(ctx)
	n := &nodeRun{key: key, cancel: cancel, done: make(chan struct{})}
	d.node = n
	tn := d.tn
	d.logf("starting the node as %s", d.saved.Label)
	go func() {
		defer close(n.done)
		n.err = brambled.Serve(nctx, tn, args, func(h *brambled.Handle) {
			d.mu.Lock()
			n.handle = h
			d.mu.Unlock()
		})
		if n.err != nil {
			d.logf("node stopped: %v", n.err)
		}
	}()
}

// ---- State ----

// State is what the UI polls.
type State struct {
	Version     string        `json:"version"`
	Phase       string        `json:"phase"`
	Problem     string        `json:"problem,omitempty"`
	PublicKey   string        `json:"publicKey"`
	Fingerprint string        `json:"fingerprint"`
	Label       string        `json:"label,omitempty"`
	Tailnet     *TailnetRef   `json:"tailnet,omitempty"`
	JoinCode    string        `json:"joinCode,omitempty"`
	Chain       *ChainInfo    `json:"chain,omitempty"`
	Node        *NodeInfo     `json:"node,omitempty"`
	Network     NetworkInfo   `json:"network"`
	Settings    SettingsInfo  `json:"settings"`
	Services    []ServiceInfo `json:"services"`
}

// ChainInfo is the registry cache's freshness.
type ChainInfo struct {
	Head      uint64    `json:"head"`
	Confirmed uint64    `json:"confirmed"`
	LastSync  time.Time `json:"lastSync"`
	Error     string    `json:"error,omitempty"`
	Endpoints []string  `json:"endpoints"`
}

// NodeInfo describes the running node.
type NodeInfo struct {
	Running  bool   `json:"running"`
	Address  string `json:"address,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	STUN     string `json:"stun,omitempty"`
}

// NetworkInfo is what the UI needs to send wallet transactions.
type NetworkInfo struct {
	ChainID          uint64 `json:"chainId"`
	NetSuffix        string `json:"netSuffix"`
	NetRegistry      string `json:"netRegistry"`
	NetRegistrar     string `json:"netRegistrar"`
	RelaySuffix      string `json:"relaySuffix"`
	RelayRegistry    string `json:"relayRegistry"`
	RelayRegistrar   string `json:"relayRegistrar"`
	ETHRegistry      string `json:"ethRegistry"`
	ExplorerTx       string `json:"explorerTx"`
	ExplorerAddress  string `json:"explorerAddress"`
	UserRegistryImpl string `json:"userRegistryImpl"`
	ResolverImpl     string `json:"resolverImpl"`
}

// SettingsInfo is the user-editable configuration.
type SettingsInfo struct {
	Ping       bool     `json:"ping"`
	RelayFirst bool     `json:"relayFirst"`
	RPCURLs    []string `json:"rpcUrls"`
}

// ServiceInfo is a service this device exposes.
type ServiceInfo struct {
	Name    string `json:"name"`
	Port    uint16 `json:"port"`
	Open    bool   `json:"open"`
	BuiltIn bool   `json:"builtIn,omitempty"`
}

func (d *Daemon) networkInfo() NetworkInfo {
	n := d.Network
	return NetworkInfo{
		ChainID: n.ChainID, NetSuffix: n.NetSuffix, NetRegistry: n.NetRegistry, NetRegistrar: n.NetRegistrar,
		RelaySuffix: n.RelaySuffix, RelayRegistry: n.RelayRegistry, RelayRegistrar: n.RelayRegistrar, ETHRegistry: n.ETHRegistry,
		ExplorerTx: "https://sepolia.etherscan.io/tx/", ExplorerAddress: "https://sepolia.etherscan.io/address/",
		UserRegistryImpl: n.UserRegistryImpl, ResolverImpl: n.ResolverImpl,
	}
}

// State returns the current state.
func (d *Daemon) State() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := State{
		Version: d.Version, Phase: d.phase, Problem: d.problem, PublicKey: d.pub, Fingerprint: Fingerprint(d.pub),
		Label: d.saved.Label, Tailnet: d.saved.Tailnet, Network: d.networkInfo(),
		Settings: SettingsInfo{Ping: !d.saved.NoPing, RelayFirst: d.saved.RelayFirst, RPCURLs: append([]string{}, d.saved.RPCURLs...)},
		Services: d.servicesLocked(),
	}
	if d.saved.Tailnet != nil && d.saved.Label != "" {
		st.JoinCode = JoinCode{Tailnet: d.saved.Tailnet.Name, Label: d.saved.Label, Pubkey: d.pub}.Encode()
	}
	if d.tn != nil {
		cs := d.tn.Status()
		st.Chain = &ChainInfo{Head: cs.Head, Confirmed: cs.Confirmed, LastSync: cs.LastSync, Error: cs.Error, Endpoints: cs.Endpoints}
	}
	if d.node != nil {
		ni := &NodeInfo{}
		select {
		case <-d.node.done:
		default:
			ni.Running = d.node.handle != nil
		}
		if h := d.node.handle; h != nil {
			ni.Address = h.Address.String()
			ep := h.Endpoint()
			ni.STUN = ep.State
			ni.Endpoint = ep.Endpoint
		}
		st.Node = ni
	}
	return st
}

func (d *Daemon) servicesLocked() []ServiceInfo {
	out := []ServiceInfo{}
	if !d.saved.NoPing && d.ping != nil {
		out = append(out, ServiceInfo{Name: PingService, Port: uint16(d.ping.port), Open: true, BuiltIn: true})
	}
	for _, s := range d.saved.Services {
		out = append(out, ServiceInfo{Name: s.Name, Port: s.Port, Open: s.Open})
	}
	return out
}

// ---- Tailnet selection ----

// Lookup resolves a tailnet name on-chain. A bare label means <label>.<net suffix>.
func (d *Daemon) Lookup(ctx context.Context, name string, start uint64) (*TailnetRef, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, errors.New("enter a tailnet name")
	}
	if !strings.Contains(name, ".") {
		name += "." + d.Network.NetSuffix
	}
	r, err := d.Network.ResolveTailnet(ctx, d.rpc, name, start)
	if err != nil {
		return nil, err
	}
	return &TailnetRef{Name: r.Name, Registry: r.Registry, Resolver: r.Resolver, Owner: r.Owner, Start: r.Start}, nil
}

// Select makes this device a member (or would-be member) of a tailnet under label.
func (d *Daemon) Select(ctx context.Context, name, label string, start uint64) error {
	if !LabelRE.MatchString(label) {
		return errors.New("a device name uses lowercase letters, digits and hyphens, up to 32 characters")
	}
	ref, err := d.Lookup(ctx, name, start)
	if err != nil {
		return err
	}
	d.mu.Lock()
	if d.saved.Tailnet == nil || d.saved.Tailnet.Registry != ref.Registry {
		d.Store.ClearCache()
	}
	d.saved.Tailnet, d.saved.Label = ref, label
	err = d.Store.SaveSaved(d.saved)
	d.mu.Unlock()
	d.poke()
	return err
}

// Leave forgets the tailnet. The device key stays.
func (d *Daemon) Leave() error {
	d.mu.Lock()
	d.saved.Tailnet, d.saved.Label = nil, ""
	err := d.Store.SaveSaved(d.saved)
	d.Store.ClearCache()
	d.mu.Unlock()
	d.poke()
	return err
}

// ---- Devices ----

// Device is one enrolled device as the UI shows it.
type Device struct {
	Label       string   `json:"label"`
	Name        string   `json:"name"`
	Self        bool     `json:"self"`
	Pubkey      string   `json:"pubkey,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	MeshIP      string   `json:"meshIp,omitempty"`
	Revoked     bool     `json:"revoked"`
	Expired     bool     `json:"expired"`
	Expiry      string   `json:"expiry"`
	Owner       string   `json:"owner"`
	Grants      int      `json:"grants"`
	ACL         []string `json:"acl"`
	Granters    []string `json:"granters"`
	Block       uint64   `json:"block"`
	// Live state from the node, when this device is a member.
	Connected bool   `json:"connected"`
	Path      string `json:"path,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	LastSeen  int64  `json:"lastSeen,omitempty"`
}

func (d *Daemon) cacheAndHandle() (*registry.Tailnet, *brambled.Handle) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var h *brambled.Handle
	if d.node != nil {
		h = d.node.handle
	}
	return d.tn, h
}

// Devices lists the tailnet's devices.
func (d *Daemon) Devices() ([]Device, error) {
	tn, h := d.cacheAndHandle()
	if tn == nil {
		return nil, errors.New("no tailnet yet")
	}
	live := map[string]statusapi.PeerStatus{}
	if h != nil {
		if peers, err := h.Peers(); err == nil {
			for _, p := range peers {
				live[p.Label] = p
			}
		}
	}
	d.mu.Lock()
	self := d.saved.Label
	d.mu.Unlock()
	out := []Device{}
	for _, r := range tn.Devices() {
		label := strings.SplitN(r.Fullname, ".", 2)[0]
		dev := Device{Label: label, Name: r.Fullname, Self: label == self, Revoked: r.Revoked, Expired: expired(r.Expiry),
			Expiry: r.Expiry, Owner: r.Owner, Grants: len(r.ACL), ACL: r.ACL, Granters: r.ACLGranters, Block: r.Block}
		if r.Pubkey != nil {
			dev.Pubkey, dev.Fingerprint = *r.Pubkey, Fingerprint(*r.Pubkey)
		}
		if r.MeshIP != nil {
			dev.MeshIP = *r.MeshIP
		}
		if p, ok := live[label]; ok {
			dev.Connected = p.Authorized && p.Handshaked && time.Since(time.Unix(0, p.LastHandshakeUnixNs)) < 3*time.Minute
			dev.Path, dev.Endpoint, dev.LastSeen = p.Path, p.Endpoint, p.LastHandshakeUnixNs/1e6
		}
		out = append(out, dev)
	}
	return out, nil
}

// NextMeshIP picks the lowest free address in the tailnet's 10.77.0.0/24.
func (d *Daemon) NextMeshIP() (string, error) {
	devs, err := d.Devices()
	if err != nil {
		return "10.77.0.1/24", nil
	}
	used := map[string]bool{}
	for _, dv := range devs {
		if p, err := netip.ParsePrefix(dv.MeshIP); err == nil {
			used[p.Addr().String()] = true
		}
	}
	for i := 1; i < 255; i++ {
		a := fmt.Sprintf("10.77.0.%d", i)
		if !used[a] {
			return a + "/24", nil
		}
	}
	return "", errors.New("the tailnet's 254 addresses are all in use")
}

// JoinRequest is a parsed join code, checked against the tailnet.
type JoinRequest struct {
	JoinCode
	Fingerprint string `json:"fingerprint"`
	MeshIP      string `json:"meshIp"`
	// Existing is set when the label is already enrolled.
	Existing *Device `json:"existing,omitempty"`
	Name     string  `json:"name"`
}

// ParseJoin decodes a join code an admin pasted.
func (d *Daemon) ParseJoin(code string) (*JoinRequest, error) {
	j, err := ParseJoinCode(code)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	ref := d.saved.Tailnet
	d.mu.Unlock()
	if ref == nil {
		return nil, errors.New("this device is not in a tailnet")
	}
	if j.Tailnet != "" && j.Tailnet != ref.Name {
		return nil, fmt.Errorf("this code is for %s, but this device manages %s", j.Tailnet, ref.Name)
	}
	req := &JoinRequest{JoinCode: j, Fingerprint: Fingerprint(j.Pubkey), Name: j.Label + "." + ref.Name}
	devs, _ := d.Devices()
	for i := range devs {
		if devs[i].Label == j.Label {
			req.Existing = &devs[i]
			req.MeshIP = devs[i].MeshIP
		}
	}
	if req.MeshIP == "" {
		if req.MeshIP, err = d.NextMeshIP(); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// GrantDigest computes the ACL digest this device (as granter) vouches for:
// device may use service on gateway (ADR 0005). It needs this device's key.
func (d *Daemon) GrantDigest(device, gw, service string) (string, error) {
	tn, _ := d.cacheAndHandle()
	if tn == nil {
		return "", errors.New("no tailnet yet")
	}
	if !ServiceRE.MatchString(service) {
		return "", errors.New("a service name uses lowercase letters, digits, - and _")
	}
	dev, err := tn.ResolveDevice(device)
	if err != nil || dev.Pubkey == nil {
		return "", fmt.Errorf("%s has no published key", device)
	}
	g, err := tn.ResolveDevice(gw)
	if err != nil || g.Pubkey == nil {
		return "", fmt.Errorf("%s has no published key", gw)
	}
	d.mu.Lock()
	priv := d.id.PrivateKeyHex
	d.mu.Unlock()
	return gateway.ACLDigestECDH(priv, *g.Pubkey, *dev.Pubkey, service)
}

// ---- Services ----

// SetServices replaces the exposed services and ping setting.
func (d *Daemon) SetServices(services []ServiceConfig, ping bool) error {
	seen := map[string]bool{PingService: true}
	for _, s := range services {
		if !ServiceRE.MatchString(s.Name) {
			return fmt.Errorf("%q: a service name uses lowercase letters, digits, - and _", s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("%q is used twice (ping is built in)", s.Name)
		}
		if s.Port == 0 {
			return fmt.Errorf("%s needs a port", s.Name)
		}
		seen[s.Name] = true
	}
	d.mu.Lock()
	d.saved.Services, d.saved.NoPing = services, !ping
	err := d.Store.SaveSaved(d.saved)
	d.mu.Unlock()
	d.poke()
	return err
}

// SetSettings changes relay routing and RPC endpoints.
func (d *Daemon) SetSettings(relayFirst bool, rpcURLs []string) error {
	var clean []string
	for _, u := range rpcURLs {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://127.0.0.1") && !strings.HasPrefix(u, "http://localhost") {
			return fmt.Errorf("%s: use an https:// endpoint", u)
		}
		clean = append(clean, u)
	}
	d.mu.Lock()
	d.saved.RelayFirst, d.saved.RPCURLs = relayFirst, clean
	d.rpc = d.newRPC(clean)
	d.stopNodeLocked()
	d.stopCacheLocked()
	err := d.Store.SaveSaved(d.saved)
	d.mu.Unlock()
	d.poke()
	return err
}

// ---- Peers ----

func (d *Daemon) handle() (*brambled.Handle, error) {
	_, h := d.cacheAndHandle()
	if h == nil {
		return nil, errors.New("this device is not connected to the tailnet yet")
	}
	return h, nil
}

// PeerServices lists the services peer lets this device use.
func (d *Daemon) PeerServices(peer string) ([]gateway.ServiceInfo, error) {
	h, err := d.handle()
	if err != nil {
		return nil, err
	}
	return h.Services(peer)
}

// PingResult is a round trip to a peer's ping service.
type PingResult struct {
	Peer    string          `json:"peer"`
	Millis  float64         `json:"millis"`
	Answer  json.RawMessage `json:"answer"`
	Path    string          `json:"path,omitempty"`
	Started time.Time       `json:"started"`
}

// Ping fetches peer's ping service through the tunnel and times it.
func (d *Daemon) Ping(peer string) (*PingResult, error) {
	h, err := d.handle()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	conn, err := h.Dial(peer, PingService)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(conn, "GET / HTTP/1.0\r\nHost: %s\r\n\r\n", peer)
	raw, err := io.ReadAll(io.LimitReader(conn, 8192))
	if err != nil {
		return nil, err
	}
	_, body, _ := strings.Cut(string(raw), "\r\n\r\n")
	res := &PingResult{Peer: peer, Millis: float64(time.Since(start).Microseconds()) / 1000, Answer: json.RawMessage(strings.TrimSpace(body)), Started: start}
	if !json.Valid(res.Answer) {
		res.Answer = json.RawMessage("null")
	}
	if peers, err := h.Peers(); err == nil {
		for _, p := range peers {
			if p.Label == peer {
				res.Path = p.Path
			}
		}
	}
	return res, nil
}

// Activity returns recent node events.
func (d *Daemon) Activity() []statusapi.ActivityEvent {
	_, h := d.cacheAndHandle()
	if h == nil {
		return []statusapi.ActivityEvent{}
	}
	return h.Activity()
}

// ---- Forwards ----

type forward struct {
	Peer    string `json:"peer"`
	Service string `json:"service"`
	Port    int    `json:"port"`
	ln      net.Listener
}

// Forward opens (or reuses) a local port that carries traffic to peer's service.
func (d *Daemon) Forward(peer, service string) (*forward, error) {
	h, err := d.handle()
	if err != nil {
		return nil, err
	}
	key := peer + "/" + service
	d.mu.Lock()
	if f, ok := d.forwards[key]; ok {
		d.mu.Unlock()
		return f, nil
	}
	d.mu.Unlock()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &forward{Peer: peer, Service: service, Port: ln.Addr().(*net.TCPAddr).Port, ln: ln}
	d.mu.Lock()
	d.forwards[key] = f
	d.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				remote, err := h.Dial(peer, service)
				if err != nil {
					d.logf("forward %s: %v", key, err)
					return
				}
				defer remote.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(remote, c); done <- struct{}{} }()
				go func() { _, _ = io.Copy(c, remote); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	return f, nil
}

// Forwards lists open forwards.
func (d *Daemon) Forwards() []*forward {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []*forward{}
	for _, f := range d.forwards {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// CloseForward closes the forward on port.
func (d *Daemon) CloseForward(port int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, f := range d.forwards {
		if f.Port == port {
			f.ln.Close()
			delete(d.forwards, k)
		}
	}
}

// ---- Relays ----

// RelayInfo is one relay from the directory with its live status.
type RelayInfo struct {
	registry.Relay
	Name   string                  `json:"name"`
	Status *rendezvous.RelayStatus `json:"status,omitempty"`
	Error  string                  `json:"error,omitempty"`
	Millis int64                   `json:"millis,omitempty"`
	// Preferred marks a relay named in the tailnet's own relays record.
	Preferred bool `json:"preferred,omitempty"`
}

// Relays lists the relay directory with each relay's status (cached for 30 s).
func (d *Daemon) Relays() ([]RelayInfo, error) {
	d.relayMu.Lock()
	defer d.relayMu.Unlock()
	if time.Since(d.relayAt) < 30*time.Second && d.relayOut != nil {
		return d.relayOut, nil
	}
	tn, _ := d.cacheAndHandle()
	var relays []registry.Relay
	var err error
	if tn != nil {
		relays, err = tn.Relays()
	} else {
		relays, err = d.directoryOnly()
	}
	if err != nil {
		return nil, err
	}
	// The directory is open; probe only the oldest entries, in parallel.
	if len(relays) > maxListedRelays {
		sort.SliceStable(relays, func(i, j int) bool { return relays[i].Block < relays[j].Block })
		relays = relays[:maxListedRelays]
	}
	preferred := map[string]bool{}
	if tn != nil {
		for _, l := range tn.PreferredRelays() {
			preferred[l] = true
		}
	}
	out := make([]RelayInfo, len(relays))
	var wg sync.WaitGroup
	for i, r := range relays {
		out[i] = RelayInfo{Relay: r, Name: r.Label + "." + d.Network.RelaySuffix, Preferred: preferred[r.Label]}
		wg.Add(1)
		go func(i int, r registry.Relay) {
			defer wg.Done()
			start := time.Now()
			st, err := rendezvous.Status(rendezvous.Relay{Label: r.Label, Address: r.Address, Pin: r.Pin}, 3*time.Second)
			if err != nil {
				out[i].Error = err.Error()
				return
			}
			out[i].Status, out[i].Millis = st, time.Since(start).Milliseconds()
		}(i, r)
	}
	wg.Wait()
	d.relayAt, d.relayOut = time.Now(), out
	return out, nil
}

const maxListedRelays = 30

// directoryOnly reads the relay directory without a tailnet (before setup).
func (d *Daemon) directoryOnly() ([]registry.Relay, error) {
	tn, err := registry.NewTailnet(registry.TailnetConfig{
		RPC: d.rpc, Name: "directory." + d.Network.Parent, Registry: d.Network.RelayRegistry, RegistryStart: d.Network.RelayStart,
		RelayRegistry: d.Network.RelayRegistry, RelayRegistryStart: d.Network.RelayStart, RelaySuffix: d.Network.RelaySuffix,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := tn.Sync(ctx); err != nil {
		return nil, err
	}
	return tn.Relays()
}

// IsAdmin reports whether address holds the registrar role on the tailnet registry.
func (d *Daemon) IsAdmin(ctx context.Context, address string) (bool, error) {
	d.mu.Lock()
	ref := d.saved.Tailnet
	d.mu.Unlock()
	if ref == nil {
		return false, nil
	}
	addr, err := registry.NormalizeAddress(address)
	if err != nil {
		return false, err
	}
	return d.rpc.HasRootRoles(ctx, ref.Registry, 1, addr)
}

// ---- Ping service ----

type pingServer struct {
	ln   net.Listener
	port int
}

// startPing serves this device's built-in ping service on loopback; the
// gateway exposes it to the tailnet as "ping".
func startPing(d *Daemon) (*pingServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		label, tailnet := d.saved.Label, ""
		if d.saved.Tailnet != nil {
			tailnet = d.saved.Tailnet.Name
		}
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pong": label, "tailnet": tailnet, "time": time.Now().UTC().Format(time.RFC3339Nano), "version": d.Version,
		})
	})}
	go func() { _ = srv.Serve(ln) }()
	return &pingServer{ln: ln, port: ln.Addr().(*net.TCPAddr).Port}, nil
}
