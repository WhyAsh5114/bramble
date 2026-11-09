package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"

	"github.com/WhyAsh5114/bramble/brambled/registry"
)

type noGrants struct{}

func (noGrants) ResolveDevice(label string) (*registry.DeviceRecord, error) {
	pub := "0000000000000000000000000000000000000000000000000000000000000001"
	return &registry.DeviceRecord{Fullname: label, Pubkey: &pub, Expiry: "4000000000"}, nil
}

func TestOpenServicesSkipTheACLAndListShowsOnlyUsableServices(t *testing.T) {
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			fmt.Fprint(c, "pong")
			c.Close()
		}
	}()
	port := uint16(backend.Addr().(*net.TCPAddr).Port)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	priv := "0000000000000000000000000000000000000000000000000000000000000002"
	s := &Server{
		Listener: ln, Resolver: noGrants{}, PrivateKeyHex: priv, OwnLabel: "gw",
		Services:   map[string]uint16{"ping": port, "db": port},
		Open:       map[string]bool{"ping": true},
		LookupPeer: func(netip.Addr) (string, bool) { return "laptop", true },
		Logf:       func(string, ...any) {},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Serve(ctx) }()

	c, _ := net.Dial("tcp", ln.Addr().String())
	list, err := ListServices(c)
	c.Close()
	if err != nil || len(list) != 1 || list[0].Name != "ping" || list[0].Access != "tailnet" {
		t.Fatalf("list %+v %v", list, err)
	}
	c, _ = net.Dial("tcp", ln.Addr().String())
	conn, err := Connect(c, "ping")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(conn); string(b) != "pong" {
		t.Fatalf("got %q", b)
	}
	c, _ = net.Dial("tcp", ln.Addr().String())
	if _, err := Connect(c, "db"); err == nil {
		t.Fatal("ungranted service was allowed")
	}
}
