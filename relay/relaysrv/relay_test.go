package relaysrv

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func startRelay(t *testing.T, limits Limits, b *Budget) (addr string, pin string) {
	t.Helper()
	dir := t.TempDir()
	key, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	pin, _ = PinOf(&key.PublicKey)
	cfg, err := TLSConfig(key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	if b == nil {
		b = NewBudget("", 0, 0)
	}
	srv := NewServer(NewDataRelay("127.0.0.1", limits, b), "127.0.0.1", "test")
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), pin
}

type client struct {
	conn net.Conn
	enc  *json.Encoder
	r    *bufio.Reader
}

func dial(t *testing.T, addr string) *client {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // tests do not check the pin
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	return &client{conn: conn, enc: json.NewEncoder(conn), r: bufio.NewReader(conn)}
}

func (c *client) send(t *testing.T, m message) {
	t.Helper()
	if err := c.enc.Encode(m); err != nil {
		t.Fatal(err)
	}
}

func (c *client) recv(t *testing.T) message {
	t.Helper()
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var defaultLimits = Limits{SessionBytes: 1 << 20, SessionTTL: time.Minute, MaxSessions: 8, SessionsPerHour: 4, PortMin: 46000, PortMax: 46099}

func TestRendezvousRoutesOpaqueOffers(t *testing.T) {
	addr, _ := startRelay(t, defaultLimits, nil)
	a, b := dial(t, addr), dial(t, addr)
	a.send(t, message{Type: "hello", Pubkey: "idA", To: "idB"})
	b.send(t, message{Type: "hello", Pubkey: "idB", To: "idA"})
	time.Sleep(50 * time.Millisecond)
	a.send(t, message{Type: "offer", To: "idB", Candidate: "sealed-blob"})
	if m := b.recv(t); m.Type != "offer" || m.From != "idA" || m.Candidate != "sealed-blob" {
		t.Fatalf("b got %+v", m)
	}
	if m := a.recv(t); m.Type != "ack" {
		t.Fatalf("a got %+v", m)
	}
}

func TestPinIsStableAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	k1, _ := LoadOrCreateKey(dir)
	k2, _ := LoadOrCreateKey(dir)
	p1, _ := PinOf(&k1.PublicKey)
	p2, _ := PinOf(&k2.PublicKey)
	if p1 != p2 {
		t.Fatal("pin changed after reload")
	}
}

func grant(t *testing.T, addr string, bytes int64) *sessionGrant {
	t.Helper()
	c := dial(t, addr)
	c.send(t, message{Type: "session", Bytes: bytes})
	m := c.recv(t)
	if m.Type != "session" || m.Session == nil {
		t.Fatalf("session refused: %+v", m)
	}
	return m.Session
}

func sessionKey(t *testing.T, g *sessionGrant) ([8]byte, []byte) {
	t.Helper()
	var id [8]byte
	raw, _ := hex.DecodeString(g.ID)
	copy(id[:], raw)
	secret, _ := hex.DecodeString(g.Secret)
	return id, secret
}

func udp(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func readWithin(c *net.UDPConn, d time.Duration) ([]byte, bool) {
	_ = c.SetReadDeadline(time.Now().Add(d))
	buf := make([]byte, 2048)
	n, _, err := c.ReadFromUDP(buf)
	if err != nil {
		return nil, false
	}
	return buf[:n], true
}

func TestDataSessionForwardsOnlyBetweenBoundPeers(t *testing.T) {
	addr, _ := startRelay(t, defaultLimits, nil)
	g := grant(t, addr, 1000)
	id, secret := sessionKey(t, g)
	relayAddr, _ := net.ResolveUDPAddr("udp", g.Endpoint)
	a, b, stranger := udp(t), udp(t), udp(t)

	// Before binding, nothing is forwarded.
	_, _ = stranger.WriteToUDP([]byte("hello?"), relayAddr)
	_, _ = a.WriteToUDP(MakeBind(secret, '0', id, time.Now()), relayAddr)
	_, _ = b.WriteToUDP(MakeBind(secret, '1', id, time.Now()), relayAddr)
	time.Sleep(50 * time.Millisecond)
	if _, ok := readWithin(a, 100*time.Millisecond); ok {
		t.Fatal("a received the stranger's packet")
	}

	_, _ = a.WriteToUDP([]byte("wireguard-ciphertext"), relayAddr)
	if got, ok := readWithin(b, time.Second); !ok || string(got) != "wireguard-ciphertext" {
		t.Fatalf("b got %q %v", got, ok)
	}
	_, _ = b.WriteToUDP([]byte("reply"), relayAddr)
	if got, ok := readWithin(a, time.Second); !ok || string(got) != "reply" {
		t.Fatalf("a got %q %v", got, ok)
	}

	// A stranger with a forged bind cannot take a slot.
	forged := MakeBind([]byte("wrong-secret-wrong-secret-wrong!"), '1', id, time.Now())
	_, _ = stranger.WriteToUDP(forged, relayAddr)
	time.Sleep(50 * time.Millisecond)
	_, _ = a.WriteToUDP([]byte("still-b"), relayAddr)
	if got, ok := readWithin(b, time.Second); !ok || string(got) != "still-b" {
		t.Fatalf("forged bind moved the slot: b got %q %v", got, ok)
	}
	if _, ok := readWithin(stranger, 100*time.Millisecond); ok {
		t.Fatal("stranger received traffic")
	}
}

func TestStaleBindIsIgnored(t *testing.T) {
	var id [8]byte
	secret := []byte("s")
	old := MakeBind(secret, '0', id, time.Now().Add(-10*time.Minute))
	if _, isBind, ok := parseBind(old, id, secret, time.Now()); !isBind || ok {
		t.Fatalf("stale bind accepted: isBind=%v ok=%v", isBind, ok)
	}
}

func TestSessionCapAndBudgetStopForwarding(t *testing.T) {
	b := NewBudget(filepath.Join(t.TempDir(), "b.json"), 50, 0)
	addr, _ := startRelay(t, defaultLimits, b)
	g := grant(t, addr, 1000)
	id, secret := sessionKey(t, g)
	relayAddr, _ := net.ResolveUDPAddr("udp", g.Endpoint)
	x, y := udp(t), udp(t)
	_, _ = x.WriteToUDP(MakeBind(secret, '0', id, time.Now()), relayAddr)
	_, _ = y.WriteToUDP(MakeBind(secret, '1', id, time.Now()), relayAddr)
	time.Sleep(50 * time.Millisecond)
	payload := make([]byte, 40)
	_, _ = x.WriteToUDP(payload, relayAddr)
	if _, ok := readWithin(y, time.Second); !ok {
		t.Fatal("first packet within budget was dropped")
	}
	_, _ = x.WriteToUDP(payload, relayAddr) // 80 bytes total, over the 50-byte day budget
	if _, ok := readWithin(y, 200*time.Millisecond); ok {
		t.Fatal("packet over the budget was forwarded")
	}
	// New sessions are refused while the budget is used up.
	c := dial(t, addr)
	c.send(t, message{Type: "session"})
	if m := c.recv(t); m.Type != "error" {
		t.Fatalf("session granted with no budget: %+v", m)
	}
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	if reloaded := NewBudget(b.path, 50, 0); !reloaded.exhausted(time.Now()) {
		t.Fatal("budget reset after restart")
	}
}

func TestSessionsPerIPAreLimited(t *testing.T) {
	addr, _ := startRelay(t, defaultLimits, nil)
	for i := 0; i < defaultLimits.SessionsPerHour; i++ {
		grant(t, addr, 0)
	}
	c := dial(t, addr)
	c.send(t, message{Type: "session"})
	if m := c.recv(t); m.Type != "error" {
		t.Fatalf("over-limit session granted: %+v", m)
	}
}

func TestGrantIsClampedToSessionCap(t *testing.T) {
	addr, _ := startRelay(t, defaultLimits, nil)
	if g := grant(t, addr, 1<<40); g.Bytes != defaultLimits.SessionBytes {
		t.Fatalf("granted %d bytes", g.Bytes)
	}
}

func TestSTUNAnswersWithTheSourceAddress(t *testing.T) {
	server := udp(t)
	go func() { _ = ServeSTUN(server) }()
	c := udp(t)
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:2], 0x0001)
	binary.BigEndian.PutUint32(req[4:8], 0x2112A442)
	copy(req[8:], "transaction!")
	_, _ = c.WriteToUDP(req, server.LocalAddr().(*net.UDPAddr))
	resp, ok := readWithin(c, time.Second)
	if !ok || len(resp) != 32 || binary.BigEndian.Uint16(resp[0:2]) != 0x0101 {
		t.Fatalf("bad STUN reply %x", resp)
	}
	port := binary.BigEndian.Uint16(resp[26:28]) ^ 0x2112
	if int(port) != c.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("mapped port %d, want %d", port, c.LocalAddr().(*net.UDPAddr).Port)
	}
}

func TestOversizedMessageClosesTheConnection(t *testing.T) {
	addr, _ := startRelay(t, defaultLimits, nil)
	c := dial(t, addr)
	big := make([]byte, maxLineBytes+10)
	for i := range big {
		big[i] = 'a'
	}
	_, _ = c.conn.Write(append(big, '\n'))
	if _, err := c.r.ReadBytes('\n'); err == nil {
		t.Fatal("relay answered an oversized message")
	}
}
