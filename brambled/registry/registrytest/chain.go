// Package registrytest is an in-memory JSON-RPC chain that emits the ENSv2
// registry and resolver events the registry cache reads. Tests in other
// packages use it to run real nodes without Sepolia.
package registrytest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/WhyAsh5114/bramble/brambled/registry"
)

// Chain is a fake Ethereum node holding logs.
type Chain struct {
	mu       sync.Mutex
	head     uint64
	logs     []registry.Log
	logIndex uint64
	nextID   uint64
	URL      string

	Registry string
	Resolver string
	Owner    string
	Suffix   string
	Start    uint64

	// The relay directory: a second registry and resolver on the same chain.
	RelayRegistry string
	RelayResolver string
	RelaySuffix   string
}

// New starts a chain with one tailnet registry and resolver.
func New(t testing.TB, suffix string) *Chain {
	c := &Chain{
		head: 100, Registry: "0x00000000000000000000000000000000000000a1", Resolver: "0x00000000000000000000000000000000000000b1",
		Owner: "0x00000000000000000000000000000000000000c1", Suffix: suffix, Start: 100, nextID: 1,
		RelayRegistry: "0x00000000000000000000000000000000000000a2", RelayResolver: "0x00000000000000000000000000000000000000b2",
		RelaySuffix: "relay.test.eth",
	}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	c.URL = srv.URL
	return c
}

// Network returns a registry.Network whose relay directory is this chain's.
func (c *Chain) Network() registry.Network {
	n := registry.Sepolia
	n.RelayRegistry, n.RelayStart, n.RelaySuffix = c.RelayRegistry, c.Start, c.RelaySuffix
	return n
}

// Config returns a TailnetConfig pointed at this chain.
func (c *Chain) Config(path string) registry.TailnetConfig {
	return registry.TailnetConfig{RPC: registry.NewRPC([]string{c.URL}), Name: c.Suffix, Registry: c.Registry, RegistryStart: c.Start, Resolver: c.Resolver, Path: path,
		RelayRegistry: c.RelayRegistry, RelayRegistryStart: c.Start, RelaySuffix: c.RelaySuffix}
}

func (c *Chain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	c.mu.Lock()
	defer c.mu.Unlock()
	var result any
	switch req.Method {
	case "eth_blockNumber":
		result = fmt.Sprintf("0x%x", c.head)
	case "eth_getLogs":
		var f struct {
			Address   []string `json:"address"`
			FromBlock string   `json:"fromBlock"`
			ToBlock   string   `json:"toBlock"`
		}
		_ = json.Unmarshal(req.Params[0], &f)
		from, _ := new(big.Int).SetString(strings.TrimPrefix(f.FromBlock, "0x"), 16)
		to, _ := new(big.Int).SetString(strings.TrimPrefix(f.ToBlock, "0x"), 16)
		want := map[string]bool{}
		for _, a := range f.Address {
			want[strings.ToLower(a)] = true
		}
		out := []registry.Log{}
		for _, l := range c.logs {
			if b := l.Block(); b >= from.Uint64() && b <= to.Uint64() && want[strings.ToLower(l.Address)] {
				out = append(out, l)
			}
		}
		result = out
	default:
		http.Error(w, "unsupported", 400)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

func word(v uint64) string { return fmt.Sprintf("%064x", v) }
func addrWord(a string) string {
	return strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(a), "0x")
}

func dynamic(values ...[]byte) string {
	headLen := len(values) * 32
	var offsets, tails []string
	off := headLen
	for _, v := range values {
		offsets = append(offsets, word(uint64(off)))
		padded := make([]byte, (len(v)+31)/32*32)
		copy(padded, v)
		tails = append(tails, word(uint64(len(v)))+hex.EncodeToString(padded))
		off += 32 + len(padded)
	}
	return "0x" + strings.Join(offsets, "") + strings.Join(tails, "")
}

func topic(sig string) string { return "0x" + hex.EncodeToString(registry.Keccak256([]byte(sig))) }

func (c *Chain) add(addr string, topics []string, data string) {
	c.logIndex++
	c.logs = append(c.logs, registry.Log{Address: addr, Topics: topics, Data: data, BlockNumber: fmt.Sprintf("0x%x", c.head), LogIndex: fmt.Sprintf("0x%x", c.logIndex)})
}

// Mine advances the head by n blocks.
func (c *Chain) Mine(n uint64) {
	c.mu.Lock()
	c.head += n
	c.mu.Unlock()
}

// Enroll registers label in the tailnet registry with the given text records, in a new block.
func (c *Chain) Enroll(label string, texts map[string]string) {
	c.enroll(c.Registry, c.Resolver, c.Suffix, label, texts)
}

// EnrollRelay lists a relay in the relay directory.
func (c *Chain) EnrollRelay(label string, texts map[string]string) {
	c.enroll(c.RelayRegistry, c.RelayResolver, c.RelaySuffix, label, texts)
}

func (c *Chain) enroll(reg, resolver, suffix, label string, texts map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.head++
	tokenID := c.nextID
	c.nextID++
	labelData := "0x" + word(96) + addrWord(c.Owner) + word(4_000_000_000)
	padded := make([]byte, (len(label)+31)/32*32)
	copy(padded, label)
	labelData += word(uint64(len(label))) + hex.EncodeToString(padded)
	c.add(reg, []string{topic("LabelRegistered(uint256,bytes32,string,address,uint64,address)"), "0x" + word(tokenID), registry.LabelHash(label), "0x" + addrWord(c.Owner)}, labelData)
	c.add(reg, []string{topic("ResolverUpdated(uint256,address,address)"), "0x" + word(tokenID), "0x" + addrWord(resolver), "0x" + addrWord(c.Owner)}, "0x")
	name := label + "." + suffix
	dns, _ := registry.DNSEncode(name)
	c.add(resolver, []string{topic("Linked(uint256,bytes32,bytes)"), "0x" + word(tokenID), "0x" + hex.EncodeToString(registry.Namehash(name))}, dynamic(dns))
	for k, v := range texts {
		c.setTextOn(resolver, tokenID, k, v)
	}
}

func (c *Chain) setTextLocked(recordID uint64, key, value string) {
	c.setTextOn(c.Resolver, recordID, key, value)
}

func (c *Chain) setTextOn(resolver string, recordID uint64, key, value string) {
	c.add(resolver, []string{topic("TextUpdated(uint256,string,string,string)"), "0x" + word(recordID), "0x" + hex.EncodeToString(registry.Keccak256([]byte(key)))}, dynamic([]byte(key), []byte(value)))
}

// SetTailnetText writes a text record on the tailnet's own name.
func (c *Chain) SetTailnetText(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.head++
	const rootRecord = 1_000_000
	dns, _ := registry.DNSEncode(c.Suffix)
	c.add(c.Resolver, []string{topic("Linked(uint256,bytes32,bytes)"), "0x" + word(rootRecord), "0x" + hex.EncodeToString(registry.Namehash(c.Suffix))}, dynamic(dns))
	c.setTextOn(c.Resolver, rootRecord, key, value)
}

// SetText writes a text record on label (enrolled earlier) in a new block.
func (c *Chain) SetText(label, key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.head++
	// Record IDs equal token IDs in this fake; find the label's token.
	for _, l := range c.logs {
		if len(l.Topics) > 2 && l.Topics[0] == topic("LabelRegistered(uint256,bytes32,string,address,uint64,address)") && l.Topics[2] == registry.LabelHash(label) {
			id, _ := new(big.Int).SetString(strings.TrimPrefix(l.Topics[1], "0x"), 16)
			c.setTextLocked(id.Uint64(), key, value)
			return
		}
	}
}
