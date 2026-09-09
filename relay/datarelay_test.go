package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

// openPeer stands in for a real WireGuard node's outer UDP socket in these
// tests — real datagrams, real sockets, no live chain (docs/adr/0008).
func openPeer(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("opening peer socket: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func recvOrFail(t *testing.T, conn *net.UDPConn, want string) {
	t.Helper()
	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("waiting for forwarded packet: %v", err)
	}
	if got := string(buf[:n]); got != want {
		t.Fatalf("forwarded payload = %q, want %q", got, want)
	}
}

func recvNothing(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadFromUDP(buf); err == nil {
		t.Fatalf("expected no forwarded packet, got one")
	}
}

func TestDataRelay_ForwardsBetweenTwoPeers(t *testing.T) {
	r := newDataRelay()
	s, err := r.allocate("127.0.0.1", 1<<20)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	relayAddr := s.conn.LocalAddr().(*net.UDPAddr)

	a, b := openPeer(t), openPeer(t)

	// a's first packet only fills slot A — nothing to forward to yet.
	if _, err := a.WriteToUDP([]byte("hello"), relayAddr); err != nil {
		t.Fatalf("a write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// b's first packet fills slot B and is immediately forwarded to a.
	if _, err := b.WriteToUDP([]byte("ping"), relayAddr); err != nil {
		t.Fatalf("b write: %v", err)
	}
	recvOrFail(t, a, "ping")

	// now both slots are known — a's next packet forwards to b.
	if _, err := a.WriteToUDP([]byte("pong"), relayAddr); err != nil {
		t.Fatalf("a write: %v", err)
	}
	recvOrFail(t, b, "pong")
}

func TestDataRelay_CutsOffAtAllotment(t *testing.T) {
	r := newDataRelay()
	s, err := r.allocate("127.0.0.1", 4) // room for exactly one 4-byte datagram
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	relayAddr := s.conn.LocalAddr().(*net.UDPAddr)

	a, b := openPeer(t), openPeer(t)
	if _, err := a.WriteToUDP([]byte("seed"), relayAddr); err != nil {
		t.Fatalf("a write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if _, err := b.WriteToUDP([]byte("ping"), relayAddr); err != nil { // 4 bytes, fits exactly
		t.Fatalf("b write: %v", err)
	}
	recvOrFail(t, a, "ping")

	if got := s.bytesRemaining(); got != 0 {
		t.Fatalf("bytesRemaining after exact fit = %d, want 0", got)
	}

	if _, err := a.WriteToUDP([]byte("pong"), relayAddr); err != nil { // over budget now
		t.Fatalf("a write: %v", err)
	}
	recvNothing(t, b)
}

// totalForwardedForAllotment allocates a fresh session with the given
// allotment, floods it with far more traffic than any allotment used here
// could hold, and returns how many bytes actually made it through — the
// operational half of Gate 4.2's "cost scales with bytes" claim.
// pricing.test.ts (relay-sidecar) proves the *price* side scales with the
// same requested byte count; this proves a bigger purchase buys strictly
// more usable forwarding capacity, not just a bigger number on an invoice.
func totalForwardedForAllotment(t *testing.T, allotment int64) int64 {
	t.Helper()
	r := newDataRelay()
	s, err := r.allocate("127.0.0.1", allotment)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	relayAddr := s.conn.LocalAddr().(*net.UDPAddr)

	a, b := openPeer(t), openPeer(t)
	payload := []byte("0123456789") // 10 bytes
	if _, err := a.WriteToUDP(payload, relayAddr); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 50; i++ { // far more than (largest allotment used) / len(payload)
		if _, err := b.WriteToUDP(payload, relayAddr); err != nil {
			t.Fatalf("b write %d: %v", i, err)
		}
	}

	var forwarded int64
	buf := make([]byte, 1024)
	for {
		_ = a.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, _, err := a.ReadFromUDP(buf)
		if err != nil {
			break
		}
		forwarded += int64(n)
	}
	return forwarded
}

func TestDataRelay_LargerAllotmentForwardsMoreBytes(t *testing.T) {
	small := totalForwardedForAllotment(t, 20)  // exactly 2 x 10-byte datagrams
	large := totalForwardedForAllotment(t, 200) // exactly 20 x 10-byte datagrams

	if small != 20 {
		t.Fatalf("small allotment forwarded %d bytes, want exactly 20 (its full allotment)", small)
	}
	if large != 200 {
		t.Fatalf("large allotment forwarded %d bytes, want exactly 200 (its full allotment)", large)
	}
	if large <= small {
		t.Fatalf("larger allotment (%d bytes forwarded) did not exceed smaller (%d) — Gate 4.2 needs capacity to scale with the purchased byte count", large, small)
	}
}

func TestDataRelay_InternalAPI_Allocates(t *testing.T) {
	r := newDataRelay()
	handler := internalAPIHandler(r, "127.0.0.1")

	body, _ := json.Marshal(internalAllocateRequest{Bytes: 1000})
	req := httptest.NewRequest("POST", "/internal/data-relay-session", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp internalAllocateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.SessionID == "" || resp.Port == 0 {
		t.Fatalf("incomplete response: %+v", resp)
	}

	// A second allocation gets a distinct session and port.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest("POST", "/internal/data-relay-session", bytes.NewReader(body)))
	var resp2 internalAllocateResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decoding second response: %v", err)
	}
	if resp2.SessionID == resp.SessionID || resp2.Port == resp.Port {
		t.Fatalf("second allocation reused the first session/port: %+v == %+v", resp2, resp)
	}
}

func TestDataRelay_InternalAPI_RejectsNonPositiveBytes(t *testing.T) {
	r := newDataRelay()
	handler := internalAPIHandler(r, "127.0.0.1")

	body, _ := json.Marshal(internalAllocateRequest{Bytes: 0})
	req := httptest.NewRequest("POST", "/internal/data-relay-session", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
