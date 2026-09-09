package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Bytes-scaled data-plane relay (docs/adr/0008): one ephemeral UDP port per
// purchased session, not a shared/multiplexed port — see the ADR's
// "wireguard-go owns the peer socket" finding for why. relay-sidecar buys a
// session on a client's behalf via the loopback-only /internal
// endpoint below after a real x402/Hedera payment settles; this file never
// touches payment or pricing, only allocation, forwarding, and accounting.

const sessionTTL = 5 * time.Minute

// dataSession is one purchased byte allotment and its forwarding socket.
// slots are learned from the first two distinct UDP source addresses seen
// on conn, not pre-registered — see the ADR's "Trust model" section for why
// that's an accepted, documented simplification here.
type dataSession struct {
	id            string
	conn          *net.UDPConn
	bytesAllotted int64
	expiresAt     time.Time

	mu        sync.Mutex
	bytesUsed int64
	slots     [2]*net.UDPAddr // first two distinct sources; nil until learned
}

// dataRelay owns every active session and the loopback allocation API.
// Public forwarding traffic never reaches allocate — each session gets its
// own socket, so there is nothing shared across sessions to protect beyond
// the session map itself.
type dataRelay struct {
	mu       sync.Mutex
	sessions map[string]*dataSession
}

func newDataRelay() *dataRelay {
	return &dataRelay{sessions: make(map[string]*dataSession)}
}

// allocate opens a fresh ephemeral UDP port, starts forwarding on it, and
// registers the session. Called only from the loopback /internal handler.
func (r *dataRelay) allocate(host string, bytes int64) (*dataSession, error) {
	if bytes <= 0 {
		return nil, fmt.Errorf("bytes must be positive, got %d", bytes)
	}

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(host), Port: 0})
	if err != nil {
		return nil, fmt.Errorf("allocating data-relay port: %w", err)
	}

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		conn.Close()
		return nil, fmt.Errorf("generating session id: %w", err)
	}

	s := &dataSession{
		id:            hex.EncodeToString(idBytes),
		conn:          conn,
		bytesAllotted: bytes,
		expiresAt:     time.Now().Add(sessionTTL),
	}

	r.mu.Lock()
	r.sessions[s.id] = s
	r.mu.Unlock()

	go r.forward(s)
	return s, nil
}

// forward is the whole relay: read one datagram, learn its source as a slot
// if one is still free, forward it to the other slot if both are known and
// the allotment isn't exhausted, repeat. Matching is exact source IP:port
// equality — a genuine NAT remap mid-session arrives as an unmatched
// source and takes the other free slot (or is dropped if both are already
// full), not silently re-bound to its old slot. This is NOT WireGuard
// roaming support; don't claim it is. Fine for a stable-NAT or localhost
// demo, a real gap for anything longer-lived.
func (r *dataRelay) forward(s *dataSession) {
	defer func() {
		s.conn.Close()
		r.mu.Lock()
		delete(r.sessions, s.id)
		r.mu.Unlock()
	}()

	buf := make([]byte, 65535)
	for {
		// Deadline is s.expiresAt itself, not a rolling window — a session
		// expires at the TTL it was allocated with regardless of how
		// recently traffic arrived (unlike, say, an idle-timeout design).
		_ = s.conn.SetReadDeadline(s.expiresAt)
		n, src, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return // deadline exceeded or socket closed — session is done
		}
		if time.Now().After(s.expiresAt) {
			return
		}

		s.mu.Lock()
		other := s.learnAndFindOther(src)
		if other == nil {
			s.mu.Unlock()
			continue // only one slot filled so far — nothing to forward to yet
		}
		if s.bytesUsed+int64(n) > s.bytesAllotted {
			s.mu.Unlock()
			continue // allotment exhausted — drop until a fresh session is bought
		}
		s.bytesUsed += int64(n)
		s.mu.Unlock()

		_, _ = s.conn.WriteToUDP(buf[:n], other)
	}
}

// learnAndFindOther assigns src to a free slot (or confirms it against its
// existing slot) and returns the other slot's address, or nil if the other
// slot isn't filled yet. Caller holds s.mu.
func (s *dataSession) learnAndFindOther(src *net.UDPAddr) *net.UDPAddr {
	for i, slot := range s.slots {
		if slot != nil && slot.IP.Equal(src.IP) && slot.Port == src.Port {
			other := s.slots[1-i]
			s.slots[i] = src // no-op unless src somehow differs by object identity only; see forward's doc comment on what this does NOT handle
			return other
		}
	}
	for i, slot := range s.slots {
		if slot == nil {
			s.slots[i] = src
			return s.slots[1-i]
		}
	}
	return nil // both slots taken by two other addresses — ignore a third source
}

// bytesRemaining reports the session's live usage, for tests and logging.
func (s *dataSession) bytesRemaining() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytesAllotted - s.bytesUsed
}

// internalAllocateRequest/Response is the loopback-only contract with
// relay-sidecar (docs/adr/0008): relay-sidecar collects payment, then calls
// this endpoint because only this Go process can open the forwarding
// socket — the mirror image of adr/0007's secret handoff (there Go hands TS
// a secret; here TS hands Go a request).
type internalAllocateRequest struct {
	Bytes int64 `json:"bytes"`
}

type internalAllocateResponse struct {
	SessionID string `json:"sessionId"`
	Port      int    `json:"port"`
	ExpiresAt int64  `json:"expiresAt"`
}

// internalAPIHandler builds the loopback-only allocation route. Separated
// from serveInternalAPI so tests can exercise it via httptest without
// binding a real listener.
func internalAPIHandler(r *dataRelay, dataHost string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/data-relay-session", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body internalAllocateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
			return
		}
		s, err := r.allocate(dataHost, body.Bytes)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalAllocateResponse{
			SessionID: s.id,
			Port:      s.conn.LocalAddr().(*net.UDPAddr).Port,
			ExpiresAt: s.expiresAt.Unix(),
		})
	})
	return mux
}

// serveInternalAPI starts the loopback-only allocation listener. Binding to
// 127.0.0.1 explicitly (not the -addr/-data-port host) is the only access
// control this endpoint has — it must never be reachable from outside the
// machine relay-sidecar runs on.
func serveInternalAPI(r *dataRelay, port int, dataHost string) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	return http.ListenAndServe(addr, internalAPIHandler(r, dataHost))
}
