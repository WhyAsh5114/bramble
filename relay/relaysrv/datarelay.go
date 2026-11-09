package relaysrv

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

// Limits bounds what one relay gives away.
type Limits struct {
	SessionBytes     int64         // cap of one session
	SessionTTL       time.Duration // lifetime of one session
	MaxSessions      int           // concurrent sessions
	SessionsPerHour  int           // new sessions per source IP per hour
	PortMin, PortMax int           // UDP ports sessions bind to (published by the host)
}

// Trace turns on per-session event logging (RELAY_TRACE=1). It never logs
// secrets or payloads, only sizes, sides and source addresses.
var Trace = os.Getenv("RELAY_TRACE") != ""

func tracef(format string, args ...any) {
	if Trace {
		log.Printf("trace: "+format, args...)
	}
}

type dataSession struct {
	id        [8]byte
	secret    []byte
	conn      *net.UDPConn
	allotted  int64
	expiresAt time.Time
	owner     string // source IP that allocated it

	mu    sync.Mutex
	used  int64
	slots [2]*net.UDPAddr
	// trace counters
	firstFwd [2]bool
	drops    map[string]int
}

type DataRelay struct {
	limits Limits
	budget *Budget
	host   string // interface sessions bind to

	mu       sync.Mutex
	sessions map[[8]byte]*dataSession
	perIP    map[string][]time.Time
	nextPort int
}

func NewDataRelay(host string, limits Limits, b *Budget) *DataRelay {
	return &DataRelay{limits: limits, budget: b, host: host, sessions: map[[8]byte]*dataSession{}, perIP: map[string][]time.Time{}, nextPort: limits.PortMin}
}

var (
	errBudget  = errors.New("relay data budget is used up for now; direct paths still work")
	errBusy    = errors.New("relay is at its session limit")
	errTooMany = errors.New("too many sessions from this address; try again later")
)

// allocate opens a session for the requester at ip. The grant is clamped to the session cap.
func (r *DataRelay) allocate(ip string, bytes int64, now time.Time) (*dataSession, error) {
	if r.budget.exhausted(now) {
		return nil, errBudget
	}
	if bytes <= 0 || bytes > r.limits.SessionBytes {
		bytes = r.limits.SessionBytes
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sessions) >= r.limits.MaxSessions {
		return nil, errBusy
	}
	recent := r.perIP[ip][:0]
	for _, t := range r.perIP[ip] {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	if r.limits.SessionsPerHour > 0 && len(recent) >= r.limits.SessionsPerHour {
		r.perIP[ip] = recent
		return nil, errTooMany
	}
	conn, err := r.listenLocked()
	if err != nil {
		return nil, err
	}
	s := &dataSession{conn: conn, allotted: bytes, expiresAt: now.Add(r.limits.SessionTTL), owner: ip, secret: make([]byte, 32)}
	if _, err := rand.Read(s.id[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := rand.Read(s.secret); err != nil {
		conn.Close()
		return nil, err
	}
	r.perIP[ip] = append(recent, now)
	r.sessions[s.id] = s
	tracef("session %s allocated for %s on port %d, %d bytes, ttl %s", s.idHex(), ip, s.port(), bytes, r.limits.SessionTTL)
	go r.forward(s)
	return s, nil
}

// listenLocked binds the next free port in the configured range.
func (r *DataRelay) listenLocked() (*net.UDPConn, error) {
	span := r.limits.PortMax - r.limits.PortMin + 1
	for i := 0; i < span; i++ {
		port := r.nextPort
		r.nextPort++
		if r.nextPort > r.limits.PortMax {
			r.nextPort = r.limits.PortMin
		}
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(r.host), Port: port})
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("no free UDP port in %d-%d", r.limits.PortMin, r.limits.PortMax)
}

func (r *DataRelay) forward(s *dataSession) {
	defer func() {
		s.conn.Close()
		r.mu.Lock()
		delete(r.sessions, s.id)
		r.mu.Unlock()
		s.mu.Lock()
		tracef("session %s closed: %d/%d bytes used, drops %v", s.idHex(), s.used, s.allotted, s.drops)
		s.mu.Unlock()
	}()
	buf := make([]byte, 65535)
	for {
		_ = s.conn.SetReadDeadline(s.expiresAt)
		n, src, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		now := time.Now()
		if now.After(s.expiresAt) {
			return
		}
		if side, isBind, ok := parseBind(buf[:n], s.id, s.secret, now); isBind {
			if ok {
				s.mu.Lock()
				prev := s.slots[side]
				s.slots[side] = src
				other := s.slots[1-side] != nil
				s.mu.Unlock()
				if prev == nil || prev.String() != src.String() {
					tracef("session %s side %d bound to %s (other side bound: %v)", s.idHex(), side, src, other)
				}
			} else {
				tracef("session %s rejected a bind from %s", s.idHex(), src)
			}
			continue
		}
		s.mu.Lock()
		from := -1
		for i, slot := range s.slots {
			if slot != nil && slot.IP.Equal(src.IP) && slot.Port == src.Port {
				from = i
			}
		}
		if from < 0 || s.slots[1-from] == nil || s.used+int64(n) > s.allotted {
			if Trace {
				reason := "unbound-source"
				switch {
				case from >= 0 && s.slots[1-from] == nil:
					reason = "peer-not-bound"
				case from >= 0:
					reason = "session-cap"
				}
				if s.drops == nil {
					s.drops = map[string]int{}
				}
				if s.drops[reason] == 0 {
					tracef("session %s dropping %d bytes from %s: %s", s.idHex(), n, src, reason)
				}
				s.drops[reason]++
			}
			s.mu.Unlock()
			continue
		}
		if !s.firstFwd[from] {
			s.firstFwd[from] = true
			tracef("session %s first forward side %d -> %d (%d bytes)", s.idHex(), from, 1-from, n)
		}
		to := s.slots[1-from]
		s.used += int64(n)
		s.mu.Unlock()
		if !r.budget.take(int64(n), now) {
			tracef("session %s: budget used up, dropping", s.idHex())
			continue
		}
		_, _ = s.conn.WriteToUDP(buf[:n], to)
	}
}

// CloseAll ends every session.
func (r *DataRelay) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		s.conn.Close()
	}
}

func (r *DataRelay) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

func (s *dataSession) port() int { return s.conn.LocalAddr().(*net.UDPAddr).Port }

func (s *dataSession) idHex() string { return hex.EncodeToString(s.id[:]) }
