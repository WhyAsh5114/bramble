package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// dataLimits bounds what one relay gives away.
type dataLimits struct {
	SessionBytes     int64         // cap of one session
	SessionTTL       time.Duration // lifetime of one session
	MaxSessions      int           // concurrent sessions
	SessionsPerHour  int           // new sessions per source IP per hour
	PortMin, PortMax int           // UDP ports sessions bind to (published by the host)
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
}

type dataRelay struct {
	limits dataLimits
	budget *budget
	host   string // interface sessions bind to

	mu       sync.Mutex
	sessions map[[8]byte]*dataSession
	perIP    map[string][]time.Time
	nextPort int
}

func newDataRelay(host string, limits dataLimits, b *budget) *dataRelay {
	return &dataRelay{limits: limits, budget: b, host: host, sessions: map[[8]byte]*dataSession{}, perIP: map[string][]time.Time{}, nextPort: limits.PortMin}
}

var (
	errBudget  = errors.New("relay data budget is used up for now; direct paths still work")
	errBusy    = errors.New("relay is at its session limit")
	errTooMany = errors.New("too many sessions from this address; try again later")
)

// allocate opens a session for the requester at ip. The grant is clamped to the session cap.
func (r *dataRelay) allocate(ip string, bytes int64, now time.Time) (*dataSession, error) {
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
	go r.forward(s)
	return s, nil
}

// listenLocked binds the next free port in the configured range.
func (r *dataRelay) listenLocked() (*net.UDPConn, error) {
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

func (r *dataRelay) forward(s *dataSession) {
	defer func() {
		s.conn.Close()
		r.mu.Lock()
		delete(r.sessions, s.id)
		r.mu.Unlock()
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
				s.slots[side] = src
				s.mu.Unlock()
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
			s.mu.Unlock()
			continue
		}
		to := s.slots[1-from]
		s.used += int64(n)
		s.mu.Unlock()
		if !r.budget.take(int64(n), now) {
			continue
		}
		_, _ = s.conn.WriteToUDP(buf[:n], to)
	}
}

func (r *dataRelay) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

func (s *dataSession) port() int { return s.conn.LocalAddr().(*net.UDPAddr).Port }

func (s *dataSession) idHex() string { return hex.EncodeToString(s.id[:]) }
