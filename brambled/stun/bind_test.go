package stun

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn"
)

func bindingReply(req []byte, mapped netip.AddrPort) []byte {
	raw := mapped.Addr().AsSlice()
	b := make([]byte, 28+len(raw))
	copy(b, req[:20])
	binary.BigEndian.PutUint16(b, 0x101)
	binary.BigEndian.PutUint16(b[2:], uint16(len(b)-20))
	binary.BigEndian.PutUint16(b[20:], 0x20)
	binary.BigEndian.PutUint16(b[22:], uint16(4+len(raw)))
	b[25] = 1
	if len(raw) == 16 {
		b[25] = 2
	}
	binary.BigEndian.PutUint16(b[26:], mapped.Port()^0x2112)
	for i := range raw {
		b[28+i] = raw[i] ^ req[4+i]
	}
	return b
}

func startBind(t *testing.T) (*Bind, uint16) {
	t.Helper()
	b := NewBind(conn.NewDefaultBind())
	b.rto = 5 * time.Millisecond
	fns, port, err := b.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			packets := make([][]byte, b.BatchSize())
			for i := range packets {
				packets[i] = make([]byte, 2048)
			}
			sizes, eps := make([]int, len(packets)), make([]conn.Endpoint, len(packets))
			for {
				if _, err := fn(packets, sizes, eps); err != nil {
					return
				}
			}
		}()
	}
	t.Cleanup(func() { b.Close(); wg.Wait() })
	return b, port
}

func fakeServer(t *testing.T, handle func(*net.UDPConn, []byte, netip.AddrPort)) *net.UDPConn {
	t.Helper()
	s, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			n, src, err := s.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			handle(s, buf[:n], src)
		}
	}()
	t.Cleanup(func() { s.Close(); <-done })
	return s
}

func TestBindingSameSocketRetransmitAndValidation(t *testing.T) {
	b, port := startBind(t)
	var requests int
	var first []byte
	observed := make(chan netip.AddrPort, 1)
	server := fakeServer(t, func(s *net.UDPConn, req []byte, src netip.AddrPort) {
		requests++
		if requests == 1 {
			first = append([]byte(nil), req...)
			return
		} // lost request
		if string(first) != string(req) {
			t.Error("retransmission changed transaction")
		}
		reply := bindingReply(req, src)
		wrong := append([]byte(nil), reply...)
		wrong[8] ^= 1
		s.WriteToUDPAddrPort(wrong, src)
		malformed := append([]byte(nil), reply...)
		malformed[3]++
		s.WriteToUDPAddrPort(malformed, src)
		impostor, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Error(err)
			return
		}
		impostor.WriteToUDPAddrPort(bindingReply(req, netip.MustParseAddrPort("192.0.2.99:9999")), src)
		impostor.Close()
		s.WriteToUDPAddrPort(reply, src)
		select {
		case observed <- src:
		default:
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	endpoint, err := b.Binding(ctx, server.LocalAddr().(*net.UDPAddr).AddrPort())
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != <-observed || endpoint.Port() != port {
		t.Fatalf("wrong mapping: %v, bind port %d", endpoint, port)
	}
}

func TestBindingCancellationCloseAndFallback(t *testing.T) {
	b, _ := startBind(t)
	gotRequest := make(chan struct{}, 1)
	silent := fakeServer(t, func(_ *net.UDPConn, _ []byte, _ netip.AddrPort) {
		select {
		case gotRequest <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	_, err := b.Binding(ctx, silent.LocalAddr().(*net.UDPAddr).AddrPort())
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	live := fakeServer(t, func(s *net.UDPConn, req []byte, src netip.AddrPort) {
		s.WriteToUDPAddrPort(bindingReply(req, src), src)
	})
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	endpoint, server, err := b.Discover(ctx, []string{silent.LocalAddr().String(), live.LocalAddr().String()})
	if err != nil || !endpoint.IsValid() || server != live.LocalAddr().String() {
		t.Fatalf("fallback: %v %s %v", endpoint, server, err)
	}
	for len(gotRequest) > 0 {
		<-gotRequest
	}
	done := make(chan error, 1)
	go func() {
		_, err := b.Binding(context.Background(), silent.LocalAddr().(*net.UDPAddr).AddrPort())
		done <- err
	}()
	<-gotRequest
	b.Close()
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close left transaction waiting")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) != 0 {
		t.Fatal("leaked transaction")
	}
}

func TestResponseAddressesAndMalformed(t *testing.T) {
	id := transactionID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	req := request(id)
	for _, value := range []string{"198.51.100.4:54321", "[2001:db8::1234]:54321"} {
		want := netip.MustParseAddrPort(value)
		wire := bindingReply(req, want)
		got, err := response(wire)
		if err != nil || got != want {
			t.Fatalf("%s: %v %v", value, got, err)
		}
		for length := 0; length < len(wire); length++ {
			if _, err := response(wire[:length]); err == nil {
				t.Fatalf("accepted truncation %d", length)
			}
		}
		wire[25] = 3
		if _, err := response(wire); err == nil {
			t.Fatal("accepted unknown address family")
		}
	}
	// Legacy MAPPED-ADDRESS is accepted when XOR-MAPPED-ADDRESS is absent.
	wire := bindingReply(req, netip.MustParseAddrPort("198.51.100.4:54321"))
	binary.BigEndian.PutUint16(wire[20:], 1)
	binary.BigEndian.PutUint16(wire[26:], 54321)
	copy(wire[28:], []byte{198, 51, 100, 4})
	if got, err := response(wire); err != nil || got.String() != "198.51.100.4:54321" {
		t.Fatalf("mapped: %v %v", got, err)
	}
}

func FuzzResponse(f *testing.F) {
	f.Add(bindingReply(request(transactionID{}), netip.MustParseAddrPort("198.51.100.4:54321")))
	f.Fuzz(func(t *testing.T, packet []byte) { _, _ = response(packet) })
}

// This literal XOR-MAPPED-ADDRESS uses the RFC 5769 section 2.2 test vector
// (without its SOFTWARE, MESSAGE-INTEGRITY and FINGERPRINT attributes).
func TestKnownWireResponse(t *testing.T) {
	packet := []byte{
		0x01, 0x01, 0x00, 0x0c, 0x21, 0x12, 0xa4, 0x42,
		0xb7, 0xe7, 0xa7, 0x01, 0xbc, 0x34, 0xd6, 0x86, 0xfa, 0x87, 0xdf, 0xae,
		0x00, 0x20, 0x00, 0x08, 0x00, 0x01, 0xa1, 0x47, 0xe1, 0x12, 0xa6, 0x43,
	}
	endpoint, err := response(packet)
	if err != nil || endpoint.String() != "192.0.2.1:32853" {
		t.Fatalf("known vector: %v %v", endpoint, err)
	}
}

type batchBind struct {
	conn.Bind
	packets [][]byte
}

func (b batchBind) Open(uint16) ([]conn.ReceiveFunc, uint16, error) {
	return []conn.ReceiveFunc{func(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		for i, p := range b.packets {
			sizes[i] = copy(packets[i], p)
		}
		return len(b.packets), nil
	}}, 12345, nil
}

func TestMixedBatchPreservesWireGuardPackets(t *testing.T) {
	wg := []byte{4, 0, 0, 0, 1, 2, 3, 4}
	b := NewBind(batchBind{Bind: conn.NewDefaultBind(), packets: [][]byte{wg, request(transactionID{}), wg}})
	fns, _, err := b.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	packets := [][]byte{make([]byte, 64), make([]byte, 64), make([]byte, 64)}
	sizes := make([]int, 3)
	n, err := fns[0](packets, sizes, make([]conn.Endpoint, 3))
	if err != nil || n != 3 || sizes[0] != len(wg) || sizes[1] != 0 || sizes[2] != len(wg) {
		t.Fatalf("batch: %d %v %v", n, sizes, err)
	}
	if string(packets[0][:sizes[0]]) != string(wg) || string(packets[2][:sizes[2]]) != string(wg) {
		t.Fatal("WireGuard payload changed")
	}
}

type sourceEndpoint struct {
	dst string
	src []byte
}

func (e *sourceEndpoint) ClearSrc()           { e.src = e.src[:0] }
func (e *sourceEndpoint) SrcToString() string { return string(e.src) }
func (e *sourceEndpoint) DstToString() string { return e.dst }
func (e *sourceEndpoint) DstIP() netip.Addr   { return netip.MustParseAddrPort(e.dst).Addr() }
func (e *sourceEndpoint) SrcIP() netip.Addr   { return netip.Addr{} }
func (e *sourceEndpoint) DstToBytes() []byte  { return []byte(e.dst) }

type routeBind struct {
	conn.Bind
	t *testing.T
}

func (routeBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	return &sourceEndpoint{dst: s}, nil
}

func (b routeBind) Send(_ [][]byte, ep conn.Endpoint) error {
	e := ep.(*sourceEndpoint)
	if len(e.src) != 0 {
		b.t.Error("send reused stale local source/interface")
	}
	e.src = append(e.src[:0], "inner-write"...) // the inner bind may cache a source on its endpoint
	return nil
}

func TestSendDoesNotMutateSharedEndpoint(t *testing.T) {
	b := NewBind(routeBind{t: t})
	shared := &sourceEndpoint{dst: "192.0.2.1:51820", src: []byte("198.51.100.7")}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if err := b.Send([][]byte{{1}}, shared); err != nil {
					t.Error(err)
					return
				}
				_ = shared.SrcToString() // WireGuard reads the shared endpoint concurrently
			}
		}()
	}
	wg.Wait()
	if string(shared.src) != "198.51.100.7" {
		t.Fatalf("shared endpoint mutated: %q", shared.src)
	}
}
