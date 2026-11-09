package registry

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Text record keys a device publishes (ADR 0004, ADR 0005, ADR 0009).
const (
	KeyPubkey      = "pubkey"
	KeyRevoked     = "revoked"
	KeyACL         = "acl"
	KeyACLGranters = "acl-granters"
	KeyMeshIP      = "mesh-ip"
)

// Text record keys a relay publishes in the relay directory (ADR 0019).
const (
	KeyRendezvous = "rendezvous" // host:port of the rendezvous listener (TCP, TLS)
	KeyPin        = "pin"        // "sha256/<base64url SPKI hash>" of the relay's TLS key
	KeyDataRelay  = "data-relay" // "on" when the relay offers data sessions
	KeySTUN       = "stun"       // host:port of the STUN server (UDP)
	KeyPrice      = "price"      // atomic units per byte; "0" is free
	KeyRegion     = "region"     // free text, e.g. "ap-south-1"
)

// DeviceRecord is what a peer needs to admit a device. Field meanings match
// the former sidecar's records so the admission and gateway code is unchanged.
type DeviceRecord struct {
	Fullname    string   `json:"fullname"`
	Pubkey      *string  `json:"pubkey"`
	Owner       string   `json:"owner"`
	Expiry      string   `json:"expiry"`
	TokenID     string   `json:"tokenId"`
	Revoked     bool     `json:"revoked"`
	ACL         []string `json:"acl"`
	ACLGranters []string `json:"aclGranters"`
	MeshIP      *string  `json:"meshIP"`
	Block       uint64   `json:"block"`
}

// Relay is one entry of the relay directory. A relay without a TLS pin is
// skipped: a client must be able to authenticate the relay it talks to.
type Relay struct {
	Label     string `json:"label"`
	Address   string `json:"address"` // rendezvous host:port
	Pin       string `json:"pin"`
	DataRelay bool   `json:"dataRelay"`
	STUN      string `json:"stun,omitempty"`
	Price     string `json:"price"`
	Region    string `json:"region,omitempty"`
	Owner     string `json:"owner"`
}

// ErrNotFound means the label is not registered in the watched registry.
var ErrNotFound = errors.New("not registered")

// Tailnet is a cache that watches one tailnet registry and, optionally, the relay directory.
type Tailnet struct {
	*Cache
	hasRelays bool
}

// TailnetConfig configures a Tailnet cache.
type TailnetConfig struct {
	RPC *RPC
	// Name is the tailnet's ENS name, e.g. "alice.net.bramble.eth".
	Name          string
	Registry      string
	RegistryStart uint64
	// RelayRegistry is the relay directory; empty disables relay discovery.
	RelayRegistry      string
	RelayRegistryStart uint64
	// RelaySuffix is the relay directory's name, e.g. "relay.bramble.eth".
	RelaySuffix string
	Path        string
}

// NewTailnet returns a cache over a tailnet registry and the relay directory.
func NewTailnet(cfg TailnetConfig) (*Tailnet, error) {
	if cfg.Name == "" || cfg.Registry == "" {
		return nil, errors.New("registry: tailnet name and registry are required")
	}
	trees := []Tree{{Registry: cfg.Registry, Suffix: cfg.Name, Start: cfg.RegistryStart}}
	hasRelays := cfg.RelayRegistry != ""
	if hasRelays {
		if cfg.RelaySuffix == "" {
			return nil, errors.New("registry: relay directory name is required")
		}
		trees = append(trees, Tree{Registry: cfg.RelayRegistry, Suffix: cfg.RelaySuffix, Start: cfg.RelayRegistryStart})
	}
	c, err := New(Config{RPC: cfg.RPC, Trees: trees, Path: cfg.Path})
	if err != nil {
		return nil, err
	}
	return &Tailnet{Cache: c, hasRelays: hasRelays}, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func deviceFrom(r *Record) *DeviceRecord {
	d := &DeviceRecord{
		Fullname:    r.Name,
		Owner:       r.Owner,
		Expiry:      strconv.FormatUint(r.Expiry, 10),
		TokenID:     r.TokenID,
		Revoked:     r.Texts[KeyRevoked] == "true",
		ACL:         splitList(r.Texts[KeyACL]),
		ACLGranters: splitList(r.Texts[KeyACLGranters]),
		Block:       r.Block,
	}
	if v := r.Texts[KeyPubkey]; v != "" {
		d.Pubkey = &v
	}
	if v := r.Texts[KeyMeshIP]; v != "" {
		d.MeshIP = &v
	}
	return d
}

// ResolveDevice returns label's record from the local cache.
func (t *Tailnet) ResolveDevice(label string) (*DeviceRecord, error) {
	r, ok := t.Lookup(0, label)
	if !ok {
		return nil, fmt.Errorf("%s: %w", label, ErrNotFound)
	}
	return deviceFrom(r), nil
}

// ListDevices returns every registered device label.
func (t *Tailnet) ListDevices() ([]string, error) {
	var out []string
	for _, r := range t.Records(0) {
		out = append(out, r.Label)
	}
	return out, nil
}

// Devices returns every registered device record.
func (t *Tailnet) Devices() []*DeviceRecord {
	var out []*DeviceRecord
	for _, r := range t.Records(0) {
		rec := r
		out = append(out, deviceFrom(&rec))
	}
	return out
}

// Relays returns the relay directory, sorted by label.
func (t *Tailnet) Relays() ([]Relay, error) {
	out := []Relay{}
	if !t.hasRelays {
		return out, nil
	}
	for _, r := range t.Records(1) {
		addr, pin := r.Texts[KeyRendezvous], r.Texts[KeyPin]
		if addr == "" || !strings.HasPrefix(pin, "sha256/") {
			continue
		}
		price := r.Texts[KeyPrice]
		if price == "" {
			price = "0"
		}
		out = append(out, Relay{
			Label: r.Label, Address: addr, Pin: pin, DataRelay: r.Texts[KeyDataRelay] == "on",
			STUN: r.Texts[KeySTUN], Price: price, Region: r.Texts[KeyRegion], Owner: r.Owner,
		})
	}
	return out, nil
}

// SyncNow runs one sync with a timeout, for callers that need fresh state now.
func (t *Tailnet) SyncNow(ctx context.Context) error { return t.Sync(ctx) }
