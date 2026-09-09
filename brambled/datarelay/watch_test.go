package datarelay

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// fakePeerLister lets tests drive RxBytes directly instead of standing up a
// real WireGuard node — WaitForStall only ever reads Peers(), so this is a
// faithful stand-in.
type fakePeerLister struct {
	mu    sync.Mutex
	rx    int64
	found bool
}

func (f *fakePeerLister) set(rx int64, found bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rx, f.found = rx, found
}

func (f *fakePeerLister) Peers() ([]wgnode.PeerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.found {
		return nil, nil
	}
	return []wgnode.PeerInfo{{PublicKeyHex: "pub", RxBytes: f.rx}}, nil
}

func TestWaitForStall_DetectsNoGrowth(t *testing.T) {
	f := &fakePeerLister{}
	f.set(100, true)

	start := time.Now()
	err := WaitForStall(context.Background(), f, "pub", 20*time.Millisecond, 100*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("WaitForStall: %v", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("returned after %s, want >= stallTimeout", elapsed)
	}
}

func TestWaitForStall_GrowthResetsTheClock(t *testing.T) {
	f := &fakePeerLister{}
	f.set(0, true)

	go func() {
		for i := int64(1); i <= 10; i++ {
			time.Sleep(15 * time.Millisecond)
			f.set(i*10, true)
		}
	}()

	start := time.Now()
	err := WaitForStall(context.Background(), f, "pub", 10*time.Millisecond, 60*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("WaitForStall: %v", err)
	}
	// Growth kept happening for ~150ms (10 * 15ms), so the stall shouldn't
	// have been detected before that window closed.
	if elapsed < 150*time.Millisecond {
		t.Fatalf("stall detected after only %s, want it to wait out the growth window", elapsed)
	}
}

func TestWaitForStall_ContextCancellation(t *testing.T) {
	f := &fakePeerLister{}
	f.set(1, true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := WaitForStall(ctx, f, "pub", 10*time.Millisecond, time.Hour)
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestWaitForStall_PeerNotYetInTableIsNotAStall(t *testing.T) {
	f := &fakePeerLister{}
	f.set(0, false) // peer absent from the table entirely

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	err := WaitForStall(ctx, f, "pub", 10*time.Millisecond, 20*time.Millisecond)
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded (should never report a stall for an absent peer)", err)
	}
}
