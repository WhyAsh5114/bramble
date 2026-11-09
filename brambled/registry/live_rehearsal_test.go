package registry

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveRehearsedTailnet reads a tailnet that scripts/rehearse-chain.ts wrote
// on Sepolia, the way a joining device does: by name, then through the cache.
// BRAMBLE_LIVE_TAILNET=<name> BRAMBLE_LIVE_LAPTOP=<pubkey> BRAMBLE_LIVE_SERVER=<pubkey>.
func TestLiveRehearsedTailnet(t *testing.T) {
	name := os.Getenv("BRAMBLE_LIVE_TAILNET")
	if name == "" {
		t.Skip("set BRAMBLE_LIVE_TAILNET")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rpc := NewRPC(nil)
	resolved, err := Sepolia.ResolveTailnet(ctx, rpc, name, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resolved %+v", resolved)
	tn, err := NewTailnet(TailnetConfig{RPC: rpc, Name: resolved.Name, Registry: resolved.Registry, RegistryStart: resolved.Start, Resolver: resolved.Resolver,
		RelayRegistry: Sepolia.RelayRegistry, RelayRegistryStart: Sepolia.RelayStart, RelaySuffix: Sepolia.RelaySuffix})
	if err != nil {
		t.Fatal(err)
	}
	if err := tn.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	laptop, err := tn.ResolveDevice("laptop")
	if err != nil {
		t.Fatal(err)
	}
	if laptop.Pubkey == nil || *laptop.Pubkey != os.Getenv("BRAMBLE_LIVE_LAPTOP") || laptop.Revoked || len(laptop.ACL) != 1 ||
		laptop.MeshIP == nil || *laptop.MeshIP != "10.77.0.1/24" || len(laptop.ACLGranters) != 1 {
		t.Fatalf("laptop %+v", laptop)
	}
	server, err := tn.ResolveDevice("server")
	if err != nil || server.Pubkey == nil || *server.Pubkey != os.Getenv("BRAMBLE_LIVE_SERVER") || !server.Revoked {
		t.Fatalf("server %+v %v", server, err)
	}
	if exp, _ := strconv.ParseInt(laptop.Expiry, 10, 64); exp < time.Now().Add(300*24*time.Hour).Unix() {
		t.Fatalf("laptop expiry %s", laptop.Expiry)
	}
	if got := tn.PreferredRelays(); len(got) != 1 || got[0] != "ap-south-1" {
		t.Fatalf("tailnet relays record %v", got)
	}
	ok, err := rpc.HasRootRoles(ctx, resolved.Registry, 1, resolved.Owner)
	if err != nil || !ok {
		t.Fatalf("owner is not admin: %v %v", ok, err)
	}
	t.Logf("all records match; owner %s is admin", resolved.Owner)
}
