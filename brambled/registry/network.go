package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Network holds the on-chain addresses Bramble reads. Values are Sepolia
// (chain 11155111); see contracts/deployments/sepolia.json.
type Network struct {
	ChainID uint64
	// ETHRegistry is ENSv2's registry for .eth.
	ETHRegistry string
	// Parent is the shared Bramble name, e.g. "bramble.eth".
	Parent string
	// NetSuffix is where open tailnet names live, e.g. "net.bramble.eth".
	NetSuffix     string
	NetRegistry   string
	NetStart      uint64
	NetRegistrar  string
	RelaySuffix   string
	RelayRegistry string
	RelayStart    uint64
	// RelayRegistrar is the open registrar relay operators claim names from.
	RelayRegistrar string
	// Factory, ResolverImpl and UserRegistryImpl are the ENSv2 implementations.
	Factory          string
	ResolverImpl     string
	UserRegistryImpl string
	MockUSDC         string
	ETHRegistrar     string
}

// Sepolia is the default network.
var Sepolia = Network{
	ChainID:          11155111,
	ETHRegistry:      "0xd4ebcbbdf463c9c45784603db0ddd499bc44a8b4",
	Parent:           "bramble.eth",
	NetSuffix:        "net.bramble.eth",
	NetRegistry:      "0xdcbbfc531da58ede81a8e122bddb271f6366954c",
	NetStart:         12097153,
	NetRegistrar:     "0x56a0a5905631e6684bed5dce2e3445af78f6b1c8",
	RelaySuffix:      "relay.bramble.eth",
	RelayRegistry:    "0x3723b26814c3590195ff89ea22e330a6f3d4f3a8",
	RelayStart:       12097160,
	RelayRegistrar:   "0x5f42c763e9d3c80a9579ad08bb3c1af0a59752a2",
	Factory:          "0xda70306c98e97ece36f997a21368e53298572991",
	ResolverImpl:     "0x115eb53f0c60696633855f90b138178fb40b2b2c",
	UserRegistryImpl: "0x9bd8a88719068d09ecee662f36c0e3856708366a",
	MockUSDC:         "0x240b0316df57887dbbe58b586508b19e633a14aa",
	ETHRegistrar:     "0xf633e7fc17e2bbe0d0965d18ec1821dcb754a3d3",
}

// Resolved is where a tailnet name lives on-chain.
type Resolved struct {
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Resolver string `json:"resolver"`
	Owner    string `json:"owner"`
	// Start is the block the name was registered in; scans begin there.
	Start uint64 `json:"start"`
}

func (r *RPC) callAddress(ctx context.Context, to, signature, label string) (string, error) {
	out, err := r.Call(ctx, to, encodeStringCall(signature, label))
	if err != nil {
		return "", err
	}
	if len(out) < 32 {
		return "", fmt.Errorf("%s: short answer", signature)
	}
	return "0x" + hexEncode(out[12:32]), nil
}

// ResolveTailnet walks the registry tree from .eth to name and returns the
// tailnet's registry, resolver and owner. For a name under the network's open
// net registry it also finds the registration block; for other names the
// caller must supply a start block (startHint), because scanning the .eth
// registry's history is too expensive for free endpoints.
func (n Network) ResolveTailnet(ctx context.Context, rpc *RPC, name string, startHint uint64) (*Resolved, error) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	labels := strings.Split(name, ".")
	if len(labels) < 2 || labels[len(labels)-1] != "eth" {
		return nil, fmt.Errorf("%q is not a .eth name", name)
	}
	registry := n.ETHRegistry
	var parent string
	for i := len(labels) - 2; i >= 1; i-- {
		next, err := rpc.callAddress(ctx, registry, "getSubregistry(string)", labels[i])
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", strings.Join(labels[i:], "."), err)
		}
		if next == zeroAddress {
			return nil, fmt.Errorf("%s has no registry", strings.Join(labels[i:], "."))
		}
		registry = next
	}
	parent = registry
	label := labels[0]
	sub, err := rpc.callAddress(ctx, parent, "getSubregistry(string)", label)
	if err != nil {
		return nil, err
	}
	if sub == zeroAddress {
		return nil, fmt.Errorf("%s is not a tailnet (no registry)", name)
	}
	resolver, err := rpc.callAddress(ctx, parent, "getResolver(string)", label)
	if err != nil {
		return nil, err
	}
	owner, err := rpc.callAddress(ctx, parent, "findOwner(string)", label)
	if err != nil {
		return nil, err
	}
	out := &Resolved{Name: name, Registry: sub, Resolver: resolver, Owner: owner, Start: startHint}
	if out.Start == 0 && strings.EqualFold(parent, n.NetRegistry) {
		start, err := n.registrationBlock(ctx, rpc, label)
		if err != nil {
			return nil, err
		}
		out.Start = start
	}
	if out.Start == 0 {
		return nil, errors.New("unknown start block: pass the block from the invite")
	}
	return out, nil
}

// registrationBlock finds the latest LabelRegistered event for label in the open net registry.
func (n Network) registrationBlock(ctx context.Context, rpc *RPC, label string) (uint64, error) {
	head, err := rpc.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	var found uint64
	for start := n.NetStart; start <= head; start += maxLogRange {
		end := start + maxLogRange - 1
		if end > head {
			end = head
		}
		logs, err := rpc.GetLogsTopics(ctx, []string{n.NetRegistry}, [][]string{{topicLabelRegistered}, nil, {LabelHash(label)}}, start, end)
		if err != nil {
			return 0, err
		}
		for _, l := range logs {
			if b := l.Block(); b > found {
				found = b
			}
		}
	}
	if found == 0 {
		return 0, fmt.Errorf("%s.%s: no registration found", label, n.NetSuffix)
	}
	return found, nil
}
