package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeChain is a JSON-RPC server holding a log list and a head. Tests edit both.
type fakeChain struct {
	mu   sync.Mutex
	head uint64
	logs []Log
	n    int
}

func (f *fakeChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	var result any
	switch req.Method {
	case "eth_blockNumber":
		result = quantity(f.head)
	case "eth_getLogs":
		var filter struct {
			Address   []string `json:"address"`
			FromBlock string   `json:"fromBlock"`
			ToBlock   string   `json:"toBlock"`
		}
		_ = json.Unmarshal(req.Params[0], &filter)
		from, _ := parseQuantity(filter.FromBlock)
		to, _ := parseQuantity(filter.ToBlock)
		want := map[string]bool{}
		for _, a := range filter.Address {
			want[strings.ToLower(a)] = true
		}
		out := []Log{}
		for _, l := range f.logs {
			if b := l.Block(); b >= from && b <= to && want[strings.ToLower(l.Address)] {
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

func (f *fakeChain) add(l Log) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, l)
}

func (f *fakeChain) setHead(h uint64) {
	f.mu.Lock()
	f.head = h
	f.mu.Unlock()
}

// dropAfter removes logs above block b, as a reorg would.
func (f *fakeChain) dropAfter(b uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keep []Log
	for _, l := range f.logs {
		if l.Block() <= b {
			keep = append(keep, l)
		}
	}
	f.logs = keep
}

const (
	testRegistry = "0x00000000000000000000000000000000000000aa"
	testResolver = "0x00000000000000000000000000000000000000bb"
	testOwner    = "0x00000000000000000000000000000000000000cc"
	testSuffix   = "alice.net.bramble.eth"
)

func word(v *big.Int) string { return fmt.Sprintf("%064x", v) }
func wordU(v uint64) string  { return word(new(big.Int).SetUint64(v)) }
func addrWord(a string) string {
	return strings.Repeat("0", 24) + strings.TrimPrefix(a, "0x")
}
func topicU(v uint64) string { return "0x" + wordU(v) }

// abiStrings encodes dynamic values (string/bytes) after the given static head words.
func abiDynamic(head []string, dyn ...[]byte) string {
	headLen := (len(head) + len(dyn)) * 32
	var offsets, tails []string
	off := headLen
	for _, d := range dyn {
		offsets = append(offsets, wordU(uint64(off)))
		padded := make([]byte, (len(d)+31)/32*32)
		copy(padded, d)
		tails = append(tails, wordU(uint64(len(d)))+hexEncode(padded))
		off += 32 + len(padded)
	}
	return "0x" + strings.Join(offsets, "") + strings.Join(head, "") + strings.Join(tails, "")
}

var logIndex uint64

func mkLog(addr string, block uint64, topics []string, data string) Log {
	logIndex++
	return Log{Address: addr, Topics: topics, Data: data, BlockNumber: quantity(block), LogIndex: quantity(logIndex)}
}

func registered(block, tokenID uint64, label string, expiry uint64) Log {
	// LabelRegistered(tokenId, labelHash, sender | string label, address owner, uint64 expiry)
	data := "0x" + wordU(96) + addrWord(testOwner) + wordU(expiry)
	padded := make([]byte, (len(label)+31)/32*32)
	copy(padded, label)
	data += wordU(uint64(len(label))) + hexEncode(padded)
	return mkLog(testRegistry, block, []string{topicLabelRegistered, topicU(tokenID), LabelHash(label), "0x" + addrWord(testOwner)}, data)
}

func resolverSet(block, tokenID uint64, resolver string) Log {
	return mkLog(testRegistry, block, []string{topicResolverUpdated, topicU(tokenID), "0x" + addrWord(resolver), "0x" + addrWord(testOwner)}, "0x")
}

func unregistered(block, tokenID uint64) Log {
	return mkLog(testRegistry, block, []string{topicLabelUnregistered, topicU(tokenID), "0x" + addrWord(testOwner)}, "0x")
}

func linked(block, recordID uint64, name string) Log {
	dns, _ := DNSEncode(name)
	return mkLog(testResolver, block, []string{topicLinked, topicU(recordID), "0x" + hexEncode(Namehash(name))}, abiDynamic(nil, dns))
}

func textSet(block, recordID uint64, key, value string) Log {
	return mkLog(testResolver, block, []string{topicTextUpdated, topicU(recordID), "0x" + hexEncode(Keccak256([]byte(key)))}, abiDynamic(nil, []byte(key), []byte(value)))
}

func newTestTailnet(t *testing.T, chain *fakeChain, path string) *Tailnet {
	t.Helper()
	srv := httptest.NewServer(chain)
	t.Cleanup(srv.Close)
	tn, err := NewTailnet(TailnetConfig{RPC: NewRPC([]string{srv.URL}), Name: testSuffix, Registry: testRegistry, RegistryStart: 100, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	tn.cfg.Confirmations = 5
	return tn
}

func enroll(chain *fakeChain, block, tokenID, recordID uint64, label, pubkey string) {
	chain.add(registered(block, tokenID, label, 4_000_000_000))
	chain.add(resolverSet(block, tokenID, testResolver))
	chain.add(linked(block, recordID, label+"."+testSuffix))
	chain.add(textSet(block, recordID, KeyPubkey, pubkey))
	chain.add(textSet(block, recordID, KeyMeshIP, "10.77.0.2/24"))
}

func TestCacheRebuildsDeviceRecords(t *testing.T) {
	chain := &fakeChain{head: 120}
	enroll(chain, 101, 1, 1, "laptop", "aa")
	tn := newTestTailnet(t, chain, "")
	if err := tn.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	d, err := tn.ResolveDevice("laptop")
	if err != nil {
		t.Fatal(err)
	}
	if d.Pubkey == nil || *d.Pubkey != "aa" || d.MeshIP == nil || *d.MeshIP != "10.77.0.2/24" || d.Revoked {
		t.Fatalf("record %+v", d)
	}
	if d.Fullname != "laptop."+testSuffix {
		t.Fatalf("fullname %q", d.Fullname)
	}
}

func TestCacheSeesRevocationInTheUnconfirmedTail(t *testing.T) {
	chain := &fakeChain{head: 120}
	enroll(chain, 101, 1, 1, "laptop", "aa")
	tn := newTestTailnet(t, chain, "")
	ctx := context.Background()
	if err := tn.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// Revoked one block ago: far shallower than the confirmation depth.
	chain.add(textSet(121, 1, KeyRevoked, "true"))
	chain.setHead(121)
	if err := tn.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	d, _ := tn.ResolveDevice("laptop")
	if !d.Revoked {
		t.Fatal("revocation in the newest block was not visible")
	}
}

func TestCacheForgetsReorgedTail(t *testing.T) {
	chain := &fakeChain{head: 120}
	enroll(chain, 101, 1, 1, "laptop", "aa")
	tn := newTestTailnet(t, chain, "")
	ctx := context.Background()
	_ = tn.Sync(ctx)
	chain.add(textSet(119, 1, KeyPubkey, "evil"))
	_ = tn.Sync(ctx)
	if d, _ := tn.ResolveDevice("laptop"); *d.Pubkey != "evil" {
		t.Fatal("tail change not applied")
	}
	// The block that carried the change is reorged away.
	chain.dropAfter(118)
	chain.setHead(121)
	_ = tn.Sync(ctx)
	if d, _ := tn.ResolveDevice("laptop"); *d.Pubkey != "aa" {
		t.Fatalf("reorged record still visible: %q", *d.Pubkey)
	}
}

func TestCacheReRegistrationDropsOldRecords(t *testing.T) {
	chain := &fakeChain{head: 200}
	enroll(chain, 101, 1, 1, "laptop", "aa")
	chain.add(unregistered(150, 1))
	// Re-registered without a resolver: the old pubkey must not come back.
	chain.add(registered(160, 2, "laptop", 4_000_000_000))
	tn := newTestTailnet(t, chain, "")
	if err := tn.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	d, err := tn.ResolveDevice("laptop")
	if err != nil {
		t.Fatal(err)
	}
	if d.Pubkey != nil {
		t.Fatalf("stale pubkey after re-registration: %q", *d.Pubkey)
	}
}

func TestCacheUnregisteredLabelIsGone(t *testing.T) {
	chain := &fakeChain{head: 200}
	enroll(chain, 101, 1, 1, "laptop", "aa")
	chain.add(unregistered(150, 1))
	tn := newTestTailnet(t, chain, "")
	_ = tn.Sync(context.Background())
	if _, err := tn.ResolveDevice("laptop"); err == nil {
		t.Fatal("unregistered device still resolves")
	}
	if labels, _ := tn.ListDevices(); len(labels) != 0 {
		t.Fatalf("labels %v", labels)
	}
}

func TestCacheReloadsFromDiskAndOnlyReadsNewBlocks(t *testing.T) {
	chain := &fakeChain{head: 120}
	enroll(chain, 101, 1, 1, "laptop", "aa")
	path := filepath.Join(t.TempDir(), "cache.json")
	tn := newTestTailnet(t, chain, path)
	if err := tn.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Drop the history: a reloaded cache must not need it.
	chain.mu.Lock()
	chain.logs = nil
	chain.mu.Unlock()
	tn2 := newTestTailnet(t, chain, path)
	if err := tn2.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d, err := tn2.ResolveDevice("laptop"); err != nil || *d.Pubkey != "aa" {
		t.Fatalf("reloaded record %+v %v", d, err)
	}
}

func TestCacheIgnoresStateForADifferentRegistry(t *testing.T) {
	chain := &fakeChain{head: 120}
	enroll(chain, 101, 1, 1, "laptop", "aa")
	path := filepath.Join(t.TempDir(), "cache.json")
	tn := newTestTailnet(t, chain, path)
	_ = tn.Sync(context.Background())
	srv := httptest.NewServer(&fakeChain{head: 120})
	defer srv.Close()
	other, err := NewTailnet(TailnetConfig{RPC: NewRPC([]string{srv.URL}), Name: "bob.net.bramble.eth", Registry: testResolver, RegistryStart: 100, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	_ = other.Sync(context.Background())
	if _, err := other.ResolveDevice("laptop"); err == nil {
		t.Fatal("cache file from another tailnet was reused")
	}
}

func TestNamehashMatchesENS(t *testing.T) {
	// namehash("eth") from EIP-137.
	if got := hexEncode(Namehash("eth")); got != "93cdeb708b7545dc668eb9280176169d1c33cfd8ed6f04690a0bcc88a93fc4ae" {
		t.Fatalf("namehash(eth) = %s", got)
	}
}
