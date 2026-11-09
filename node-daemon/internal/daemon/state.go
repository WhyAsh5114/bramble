package daemon

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Saved is what the daemon keeps across restarts (state.json).
type Saved struct {
	Tailnet *TailnetRef `json:"tailnet,omitempty"`
	// Label is this device's name in the tailnet.
	Label    string          `json:"label,omitempty"`
	Services []ServiceConfig `json:"services"`
	// NoPing turns the built-in ping service off.
	NoPing bool `json:"noPing,omitempty"`
	// RelayFirst routes every peer through a relay (for networks that block UDP hole punching).
	RelayFirst bool     `json:"relayFirst,omitempty"`
	RPCURLs    []string `json:"rpcUrls,omitempty"`
	// Pay turns on paying relays that charge for data sessions (ADR 0021).
	Pay *PaySettings `json:"pay,omitempty"`
}

// TailnetRef is where a tailnet lives on-chain.
type TailnetRef struct {
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Resolver string `json:"resolver"`
	Owner    string `json:"owner"`
	Start    uint64 `json:"start"`
}

// ServiceConfig is one local port this device exposes to the tailnet.
type ServiceConfig struct {
	Name string `json:"name"`
	Port uint16 `json:"port"`
	// Open lets every admitted device use it; otherwise a device needs a grant.
	Open bool `json:"open"`
}

// LabelRE matches a device or tailnet label: lowercase letters, digits, inner hyphens.
var LabelRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// ServiceRE matches a service name.
var ServiceRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// PingService is the built-in service every device exposes unless turned off.
const PingService = "ping"

// Fingerprint is the first 16 hex digits of a public key in groups of four,
// so two people can compare the key they see.
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

// JoinCode is what a joining device shows its admin: no server carries it.
type JoinCode struct {
	Tailnet string `json:"t"`
	Label   string `json:"l"`
	Pubkey  string `json:"k"`
}

const joinCodePrefix = "bramble-join:1:"

// Encode renders the code as copyable text.
func (j JoinCode) Encode() string {
	raw, _ := json.Marshal(j)
	return joinCodePrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// ParseJoinCode decodes and validates a join code.
func ParseJoinCode(s string) (JoinCode, error) {
	var j JoinCode
	body, ok := strings.CutPrefix(strings.TrimSpace(s), joinCodePrefix)
	if !ok {
		return j, errors.New("this is not a Bramble join code; it starts with " + joinCodePrefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || json.Unmarshal(raw, &j) != nil {
		return j, errors.New("the join code is damaged; copy it again")
	}
	if !LabelRE.MatchString(j.Label) {
		return j, fmt.Errorf("the join code has an invalid device name %q", j.Label)
	}
	if len(j.Pubkey) != 64 || strings.Trim(strings.ToLower(j.Pubkey), "0123456789abcdef") != "" {
		return j, errors.New("the join code has an invalid key")
	}
	j.Pubkey = strings.ToLower(j.Pubkey)
	j.Tailnet = strings.ToLower(j.Tailnet)
	return j, nil
}
