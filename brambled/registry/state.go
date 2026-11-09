package registry

import (
	"fmt"
	"sort"
	"strings"
)

// Entry is one label in a watched registry, rebuilt from registry events.
type Entry struct {
	Label       string `json:"label"`
	TokenID     string `json:"tokenId"`
	Owner       string `json:"owner"`
	Expiry      uint64 `json:"expiry"`
	Resolver    string `json:"resolver"`
	Subregistry string `json:"subregistry"`
	Registered  bool   `json:"registered"`
	// Block is where the label was (last) registered.
	Block uint64 `json:"block"`
}

// treeState is the event-sourced view of one registry.
type treeState struct {
	Suffix string            `json:"suffix"`
	Start  uint64            `json:"start"`
	Labels map[string]*Entry `json:"labels"` // labelhash -> entry
	Tokens map[string]string `json:"tokens"` // tokenId (decimal) -> labelhash
}

// resolverState is the event-sourced view of one PermissionedResolver.
type resolverState struct {
	Links map[string]string            `json:"links"` // namehash hex -> recordId
	Texts map[string]map[string]string `json:"texts"` // recordId -> key -> value
	// Synced is the last block whose logs this resolver has absorbed.
	Synced uint64 `json:"synced"`
}

// state is everything the cache knows, as of Block.
type state struct {
	Block     uint64                    `json:"block"`
	Trees     map[string]*treeState     `json:"trees"`     // registry address -> tree
	Resolvers map[string]*resolverState `json:"resolvers"` // resolver address -> records
}

func newState() *state {
	return &state{Trees: map[string]*treeState{}, Resolvers: map[string]*resolverState{}}
}

func (s *state) clone() *state {
	out := &state{Block: s.Block, Trees: make(map[string]*treeState, len(s.Trees)), Resolvers: make(map[string]*resolverState, len(s.Resolvers))}
	for addr, t := range s.Trees {
		nt := &treeState{Suffix: t.Suffix, Start: t.Start, Labels: make(map[string]*Entry, len(t.Labels)), Tokens: make(map[string]string, len(t.Tokens))}
		for k, e := range t.Labels {
			ce := *e
			nt.Labels[k] = &ce
		}
		for k, v := range t.Tokens {
			nt.Tokens[k] = v
		}
		out.Trees[addr] = nt
	}
	for addr, r := range s.Resolvers {
		nr := &resolverState{Synced: r.Synced, Links: make(map[string]string, len(r.Links)), Texts: make(map[string]map[string]string, len(r.Texts))}
		for k, v := range r.Links {
			nr.Links[k] = v
		}
		for id, kv := range r.Texts {
			m := make(map[string]string, len(kv))
			for k, v := range kv {
				m[k] = v
			}
			nr.Texts[id] = m
		}
		out.Resolvers[addr] = nr
	}
	return out
}

// resolverAddresses lists every resolver a watched label points at.
func (s *state) resolverAddresses() []string {
	seen := map[string]bool{}
	for _, t := range s.Trees {
		for _, e := range t.Labels {
			if e.Registered && e.Resolver != "" && e.Resolver != zeroAddress {
				seen[e.Resolver] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

const zeroAddress = "0x0000000000000000000000000000000000000000"

// sortLogs orders logs by chain position.
func sortLogs(logs []Log) {
	sort.SliceStable(logs, func(i, j int) bool {
		bi, ii := logs[i].Position()
		bj, ij := logs[j].Position()
		if bi != bj {
			return bi < bj
		}
		return ii < ij
	})
}

// apply folds one log into the state. Logs from addresses the state does not
// watch are ignored. A malformed log is skipped: the chain decides, not us.
func (s *state) apply(l Log) error {
	if l.Removed || len(l.Topics) == 0 {
		return nil
	}
	addr := strings.ToLower(l.Address)
	if t, ok := s.Trees[addr]; ok {
		return t.apply(l)
	}
	if r, ok := s.Resolvers[addr]; ok {
		if err := r.apply(l); err != nil {
			return err
		}
		if b := l.Block(); b > r.Synced {
			r.Synced = b
		}
	}
	return nil
}

func (t *treeState) entryFor(tokenID string) *Entry {
	if lh, ok := t.Tokens[tokenID]; ok {
		return t.Labels[lh]
	}
	return nil
}

func (t *treeState) apply(l Log) error {
	data, err := hexDecode(l.Data)
	if err != nil {
		return err
	}
	d := abiData(data)
	switch l.Topics[0] {
	case topicLabelRegistered:
		if len(l.Topics) < 3 {
			return fmt.Errorf("LabelRegistered: %d topics", len(l.Topics))
		}
		raw, err := d.bytesAt(0)
		if err != nil {
			return err
		}
		owner, err := d.addressAt(1)
		if err != nil {
			return err
		}
		expiry, err := d.uint64At(2)
		if err != nil {
			return err
		}
		label := string(raw)
		lh := strings.ToLower(l.Topics[2])
		tokenID := topicUint(l.Topics[1])
		e := t.Labels[lh]
		if e == nil {
			e = &Entry{}
			t.Labels[lh] = e
		}
		// A re-registration starts from scratch: a new owner never inherits a resolver.
		*e = Entry{Label: label, TokenID: tokenID, Owner: owner, Expiry: expiry, Registered: true, Block: l.Block()}
		t.Tokens[tokenID] = lh
	case topicLabelUnregistered:
		if e := t.entryFor(topicUint(l.Topics[1])); e != nil {
			e.Registered = false
			e.Resolver = ""
			e.Subregistry = ""
		}
	case topicExpiryUpdated:
		if len(l.Topics) < 3 {
			return nil
		}
		if e := t.entryFor(topicUint(l.Topics[1])); e != nil {
			b, _ := hexDecode(l.Topics[2])
			e.Expiry = new(bigUint).set(b)
		}
	case topicResolverUpdated:
		if len(l.Topics) < 3 {
			return nil
		}
		if e := t.entryFor(topicUint(l.Topics[1])); e != nil {
			e.Resolver = topicAddress(l.Topics[2])
		}
	case topicSubregistryUpdated:
		if len(l.Topics) < 3 {
			return nil
		}
		if e := t.entryFor(topicUint(l.Topics[1])); e != nil {
			e.Subregistry = topicAddress(l.Topics[2])
		}
	case topicTokenRegenerated:
		if len(l.Topics) < 3 {
			return nil
		}
		oldID, newID := topicUint(l.Topics[1]), topicUint(l.Topics[2])
		if lh, ok := t.Tokens[oldID]; ok {
			t.Tokens[newID] = lh
			if e := t.Labels[lh]; e != nil {
				e.TokenID = newID
			}
		}
	}
	return nil
}

func (r *resolverState) apply(l Log) error {
	data, err := hexDecode(l.Data)
	if err != nil {
		return err
	}
	d := abiData(data)
	switch l.Topics[0] {
	case topicLinked:
		if len(l.Topics) < 3 {
			return nil
		}
		r.Links[strings.ToLower(l.Topics[2])] = topicUint(l.Topics[1])
	case topicTextUpdated:
		if len(l.Topics) < 2 {
			return nil
		}
		key, err := d.bytesAt(0)
		if err != nil {
			return err
		}
		value, err := d.bytesAt(1)
		if err != nil {
			return err
		}
		id := topicUint(l.Topics[1])
		if r.Texts[id] == nil {
			r.Texts[id] = map[string]string{}
		}
		r.Texts[id][string(key)] = string(value)
	case topicCleared:
		delete(r.Texts, topicUint(l.Topics[1]))
	}
	return nil
}

// texts returns the text records of name on resolver, or nil.
func (s *state) texts(resolver, name string) map[string]string {
	r := s.Resolvers[resolver]
	if r == nil {
		return nil
	}
	id, ok := r.Links["0x"+hexEncode(Namehash(name))]
	if !ok {
		return nil
	}
	return r.Texts[id]
}

// bigUint converts a big-endian word to uint64, saturating.
type bigUint struct{}

func (bigUint) set(b []byte) uint64 {
	var n uint64
	for i, c := range b {
		if len(b)-i > 8 && c != 0 {
			return ^uint64(0)
		}
		n = n<<8 | uint64(c)
	}
	return n
}
