// Package registry keeps a node's own cache of its tailnet registry, the
// resolvers that registry points at, and the relay directory (ADR 0019).
//
// The cache is event-sourced. Each sync fetches eth_getLogs from the last
// confirmed block to the head: one call per 9,000 blocks, whatever the member
// count. Logs at least Confirmations deep fold into the confirmed state, which
// is saved to disk. Newer logs are re-applied to a copy on every sync, so a
// reorg in the newest blocks never leaves stale records behind.
//
// The cache is per node on purpose. A shared, hosted indexer would be a
// trusted coordinator; here every peer reads the chain itself.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxLogRange bounds one getLogs window; common free tiers cap at 10,000.
const maxLogRange = 9_000

// Tree names one registry to watch.
type Tree struct {
	// Registry is the registry contract address.
	Registry string
	// Suffix is the name the registry's labels live under, e.g. "alice.net.bramble.eth".
	Suffix string
	// Start is the first block to scan (the registry's deployment block).
	Start uint64
}

// Config configures a Cache.
type Config struct {
	RPC   *RPC
	Trees []Tree
	// Path is the file the confirmed state is saved to. Empty keeps it in memory.
	Path string
	// Confirmations is how deep a log must be before it is saved. Default 32.
	Confirmations uint64
	// Interval is the time between syncs in Run. Default 4 s.
	Interval time.Duration
}

// Cache is a node's local, event-sourced copy of the registries it watches.
type Cache struct {
	cfg Config
	key string

	syncMu    sync.Mutex // one sync at a time
	mu        sync.RWMutex
	confirmed *state
	view      *state
	head      uint64
	lastSync  time.Time
	lastErr   error
	ready     chan struct{}
	readyOnce sync.Once
}

// New returns a cache for cfg, loading any saved state that matches it.
func New(cfg Config) (*Cache, error) {
	if cfg.RPC == nil {
		cfg.RPC = NewRPC(nil)
	}
	if cfg.Confirmations == 0 {
		cfg.Confirmations = 32
	}
	if cfg.Interval == 0 {
		cfg.Interval = 4 * time.Second
	}
	if len(cfg.Trees) == 0 {
		return nil, errors.New("registry: no registry to watch")
	}
	st := newState()
	var keyParts []string
	for i, t := range cfg.Trees {
		addr, err := NormalizeAddress(t.Registry)
		if err != nil {
			return nil, err
		}
		cfg.Trees[i].Registry = addr
		cfg.Trees[i].Suffix = strings.ToLower(strings.TrimSuffix(t.Suffix, "."))
		st.Trees[addr] = &treeState{Suffix: cfg.Trees[i].Suffix, Start: t.Start, Labels: map[string]*Entry{}, Tokens: map[string]string{}}
		keyParts = append(keyParts, fmt.Sprintf("%s@%s#%d", addr, cfg.Trees[i].Suffix, t.Start))
	}
	sort.Strings(keyParts)
	c := &Cache{cfg: cfg, key: strings.Join(keyParts, ","), ready: make(chan struct{})}
	st.Block = c.startBlock() - 1
	if saved, ok := c.load(); ok {
		st = saved
	}
	c.confirmed = st
	c.view = st.clone()
	return c, nil
}

func (c *Cache) startBlock() uint64 {
	min := ^uint64(0)
	for _, t := range c.cfg.Trees {
		if t.Start < min {
			min = t.Start
		}
	}
	if min == 0 {
		min = 1
	}
	return min
}

type savedFile struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
	State   *state `json:"state"`
}

func (c *Cache) load() (*state, bool) {
	if c.cfg.Path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(c.cfg.Path)
	if err != nil {
		return nil, false
	}
	var f savedFile
	if json.Unmarshal(raw, &f) != nil || f.Version != 1 || f.Key != c.key || f.State == nil {
		return nil, false
	}
	return f.State, true
}

func (c *Cache) save(st *state) error {
	if c.cfg.Path == "" {
		return nil
	}
	raw, err := json.Marshal(savedFile{Version: 1, Key: c.key, State: st})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.cfg.Path), 0o700); err != nil {
		return err
	}
	tmp := c.cfg.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.cfg.Path)
}

// getLogs pages [from, to] in windows a free endpoint accepts, halving a window it refuses.
func (c *Cache) getLogs(ctx context.Context, addrs []string, from, to uint64) ([]Log, error) {
	var out []Log
	span := uint64(maxLogRange)
	for start := from; start <= to; {
		end := start + span - 1
		if end > to {
			end = to
		}
		logs, err := c.cfg.RPC.GetLogs(ctx, addrs, start, end)
		if err != nil {
			if rangeTooLarge(err) && span > 50 {
				span /= 2
				continue
			}
			return nil, err
		}
		out = append(out, logs...)
		start = end + 1
	}
	return out, nil
}

// Sync brings the cache up to the chain head once.
func (c *Cache) Sync(ctx context.Context) error {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	err := c.sync(ctx)
	c.mu.Lock()
	c.lastErr = err
	if err == nil {
		c.lastSync = time.Now()
	}
	c.mu.Unlock()
	if err == nil {
		c.readyOnce.Do(func() { close(c.ready) })
	}
	return err
}

func (c *Cache) sync(ctx context.Context) error {
	head, err := c.cfg.RPC.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("reading head: %w", err)
	}
	c.mu.RLock()
	confirmed := c.confirmed.clone()
	c.mu.RUnlock()
	if head <= confirmed.Block {
		return nil
	}
	safe := confirmed.Block
	if head > c.cfg.Confirmations && head-c.cfg.Confirmations > safe {
		safe = head - c.cfg.Confirmations
	}

	// One query covers the registries and every resolver already known.
	var watched []string
	for addr := range confirmed.Trees {
		watched = append(watched, addr)
	}
	known := map[string]bool{}
	for addr := range confirmed.Resolvers {
		known[addr] = true
		watched = append(watched, addr)
	}
	sort.Strings(watched)
	logs, err := c.getLogs(ctx, watched, confirmed.Block+1, head)
	if err != nil {
		return fmt.Errorf("logs: %w", err)
	}
	sortLogs(logs)

	// Registry logs can point at resolvers this cache has never read.
	probe := confirmed.clone()
	for _, l := range logs {
		_ = probe.apply(l)
	}
	var fresh []string
	for _, a := range append(confirmed.resolverAddresses(), probe.resolverAddresses()...) {
		if !known[a] {
			known[a] = true
			fresh = append(fresh, a)
		}
	}
	sort.Strings(fresh)
	// A new resolver may hold records written before it was attached, so it is
	// backfilled from the start into the confirmed state, then read to the head.
	for _, a := range fresh {
		rs := &resolverState{Links: map[string]string{}, Texts: map[string]map[string]string{}}
		confirmed.Resolvers[a] = rs
		if confirmed.Block >= c.startBlock() {
			old, err := c.getLogs(ctx, []string{a}, c.startBlock(), confirmed.Block)
			if err != nil {
				return fmt.Errorf("backfilling resolver %s: %w", a, err)
			}
			sortLogs(old)
			for _, l := range old {
				_ = rs.apply(l)
			}
		}
		rs.Synced = confirmed.Block
	}
	if len(fresh) > 0 {
		more, err := c.getLogs(ctx, fresh, confirmed.Block+1, head)
		if err != nil {
			return fmt.Errorf("resolver logs: %w", err)
		}
		logs = append(logs, more...)
		sortLogs(logs)
	}
	next := confirmed
	view := (*state)(nil)
	for _, l := range logs {
		if l.Block() > safe {
			break
		}
		_ = next.apply(l)
	}
	next.Block = safe
	for _, r := range next.Resolvers {
		if r.Synced < safe {
			r.Synced = safe
		}
	}
	view = next.clone()
	for _, l := range logs {
		if l.Block() > safe {
			_ = view.apply(l)
		}
	}
	if err := c.save(next); err != nil {
		return fmt.Errorf("saving cache: %w", err)
	}
	c.mu.Lock()
	c.confirmed = next
	c.view = view
	c.head = head
	c.mu.Unlock()
	return nil
}

// Run syncs until ctx ends. Errors are kept for Status and retried next interval.
func (c *Cache) Run(ctx context.Context) {
	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()
	for {
		_ = c.Sync(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// WaitReady blocks until the first successful sync or ctx ends.
func (c *Cache) WaitReady(ctx context.Context) error {
	select {
	case <-c.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Status describes the cache's freshness.
type Status struct {
	Head      uint64    `json:"head"`
	Confirmed uint64    `json:"confirmed"`
	LastSync  time.Time `json:"lastSync"`
	Error     string    `json:"error,omitempty"`
	Endpoints []string  `json:"endpoints"`
}

// Status returns the cache's freshness.
func (c *Cache) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := Status{Head: c.head, Confirmed: c.confirmed.Block, LastSync: c.lastSync}
	if c.lastErr != nil {
		s.Error = c.lastErr.Error()
	}
	for _, u := range c.cfg.RPC.Endpoints() {
		s.Endpoints = append(s.Endpoints, hostOf(u))
	}
	return s
}

// Record is a label in a watched registry together with its text records.
type Record struct {
	Entry
	Name  string            `json:"name"`
	Texts map[string]string `json:"texts"`
}

// Lookup returns label's record in the registry at index tree of Config.Trees.
func (c *Cache) Lookup(tree int, label string) (*Record, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t := c.view.Trees[c.cfg.Trees[tree].Registry]
	if t == nil {
		return nil, false
	}
	e := t.Labels[LabelHash(label)]
	if e == nil || !e.Registered {
		return nil, false
	}
	return c.recordLocked(t, e), true
}

// Records lists every registered label of tree, sorted by label.
func (c *Cache) Records(tree int) []Record {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t := c.view.Trees[c.cfg.Trees[tree].Registry]
	if t == nil {
		return nil
	}
	var out []Record
	for _, e := range t.Labels {
		if e.Registered {
			out = append(out, *c.recordLocked(t, e))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func (c *Cache) recordLocked(t *treeState, e *Entry) *Record {
	name := e.Label + "." + t.Suffix
	texts := map[string]string{}
	for k, v := range c.view.texts(e.Resolver, name) {
		texts[k] = v
	}
	return &Record{Entry: *e, Name: name, Texts: texts}
}
