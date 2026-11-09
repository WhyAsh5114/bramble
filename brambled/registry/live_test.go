package registry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLiveMumbaiTailnet syncs the real Mumbai tailnet from Sepolia.
// Run with BRAMBLE_LIVE=1; it makes a few dozen public RPC calls.
func TestLiveMumbaiTailnet(t *testing.T) {
	if os.Getenv("BRAMBLE_LIVE") == "" {
		t.Skip("set BRAMBLE_LIVE=1 to read Sepolia")
	}
	path := filepath.Join(t.TempDir(), "cache.json")
	cfg := TailnetConfig{
		Name:               "bramble-mumbai-20261107.eth",
		Registry:           "0xF9c3cEa16cD75A1E8efBEE89fF0C1c04aB3F4148",
		RegistryStart:      12078393,
		RelayRegistry:      "0x94F99d10eda494aF86D7d6E8431Ecd189E97412d",
		RelayRegistryStart: 12078414,
		RelaySuffix:        "relays.bramble-mumbai-20261107.eth",
		Path:               path,
	}
	tn, err := NewTailnet(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	if err := tn.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("first sync %s, status %+v", time.Since(start), tn.Status())
	labels, _ := tn.ListDevices()
	t.Logf("devices: %v", labels)
	gw, err := tn.ResolveDevice("mumbai-gateway")
	if err != nil {
		t.Fatal(err)
	}
	if gw.Pubkey == nil || gw.MeshIP == nil {
		t.Fatalf("gateway record incomplete: %+v", gw)
	}
	t.Logf("gateway pubkey %s mesh %s granters %v revoked %v", *gw.Pubkey, *gw.MeshIP, gw.ACLGranters, gw.Revoked)
	relays, _ := tn.Relays()
	t.Logf("relays: %+v", relays)

	// A restart loads the saved state and only fetches new blocks.
	tn2, err := NewTailnet(cfg)
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if err := tn2.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("second sync from disk %s", time.Since(start))
	gw2, err := tn2.ResolveDevice("mumbai-gateway")
	if err != nil || *gw2.Pubkey != *gw.Pubkey {
		t.Fatalf("reloaded record differs: %v %+v", err, gw2)
	}
}
