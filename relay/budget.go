package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// budget counts bytes the data relay forwards against a daily and a monthly
// cap. When either is reached the relay stops forwarding data until the
// period rolls over; rendezvous keeps working, so direct paths still form.
// Counters are saved to disk so a restart does not reset them.
type budget struct {
	mu       sync.Mutex
	path     string
	perDay   int64
	perMonth int64
	state    budgetState
	dirty    bool
}

type budgetState struct {
	Day        string `json:"day"`   // 2006-01-02, UTC
	Month      string `json:"month"` // 2006-01, UTC
	DayBytes   int64  `json:"dayBytes"`
	MonthBytes int64  `json:"monthBytes"`
}

func newBudget(path string, perDay, perMonth int64) *budget {
	b := &budget{path: path, perDay: perDay, perMonth: perMonth}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &b.state)
		}
	}
	b.roll(time.Now())
	return b
}

func (b *budget) roll(now time.Time) {
	day, month := now.UTC().Format("2006-01-02"), now.UTC().Format("2006-01")
	if b.state.Day != day {
		b.state.Day, b.state.DayBytes, b.dirty = day, 0, true
	}
	if b.state.Month != month {
		b.state.Month, b.state.MonthBytes, b.dirty = month, 0, true
	}
}

// take reserves n bytes. It returns false when a cap would be exceeded.
func (b *budget) take(n int64, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(now)
	// A packet that does not fit pauses forwarding for the rest of the period,
	// so the relay stops cleanly instead of trickling small packets through.
	if b.perDay > 0 && b.state.DayBytes+n > b.perDay {
		b.state.DayBytes, b.dirty = b.perDay, true
		return false
	}
	if b.perMonth > 0 && b.state.MonthBytes+n > b.perMonth {
		b.state.MonthBytes, b.dirty = b.perMonth, true
		return false
	}
	b.state.DayBytes += n
	b.state.MonthBytes += n
	b.dirty = true
	return true
}

// exhausted reports whether no data can be forwarded right now.
func (b *budget) exhausted(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(now)
	return (b.perDay > 0 && b.state.DayBytes >= b.perDay) || (b.perMonth > 0 && b.state.MonthBytes >= b.perMonth)
}

// BudgetStatus is reported by the relay's status message.
type BudgetStatus struct {
	DayBytes         int64 `json:"dayBytes"`
	DayLimit         int64 `json:"dayLimit"`
	MonthBytes       int64 `json:"monthBytes"`
	MonthLimit       int64 `json:"monthLimit"`
	ForwardingPaused bool  `json:"forwardingPaused"`
}

func (b *budget) status(now time.Time) BudgetStatus {
	paused := b.exhausted(now)
	b.mu.Lock()
	defer b.mu.Unlock()
	return BudgetStatus{DayBytes: b.state.DayBytes, DayLimit: b.perDay, MonthBytes: b.state.MonthBytes, MonthLimit: b.perMonth, ForwardingPaused: paused}
}

// save writes the counters if they changed.
func (b *budget) save() error {
	b.mu.Lock()
	if !b.dirty || b.path == "" {
		b.mu.Unlock()
		return nil
	}
	raw, err := json.Marshal(b.state)
	b.dirty = false
	b.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}
