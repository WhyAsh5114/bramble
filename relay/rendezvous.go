package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// The rendezvous protocol is newline-delimited JSON over TLS (ADR 0019).
//
// Peers register under pseudonymous IDs derived from their key pair and send
// sealed candidates (brambled/rendezvous). The relay routes an "offer" from ID
// A addressed to ID B to the connection that registered as B for A. It cannot
// read or forge candidates and never learns WireGuard keys.
type message struct {
	Type      string `json:"type"`
	Pubkey    string `json:"pubkey,omitempty"` // the sender's pseudonymous ID
	To        string `json:"to,omitempty"`
	From      string `json:"from,omitempty"`
	Candidate string `json:"candidate,omitempty"` // sealed, opaque to the relay
	Message   string `json:"message,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`

	Session *sessionGrant `json:"session,omitempty"`
	Status  *relayStatus  `json:"status,omitempty"`
}

type sessionGrant struct {
	ID        string `json:"id"`
	Endpoint  string `json:"endpoint"` // public host:port for WireGuard traffic
	Secret    string `json:"secret"`   // hex; proves membership in bind packets
	Bytes     int64  `json:"bytes"`
	ExpiresAt int64  `json:"expiresAt"`
}

type relayStatus struct {
	Version  string       `json:"version"`
	Peers    int          `json:"peers"`
	Sessions int          `json:"sessions"`
	Budget   BudgetStatus `json:"budget"`
	Free     bool         `json:"free"`
}

const (
	maxLineBytes     = 16 << 10
	maxConnsPerIP    = 32
	messagesPerSec   = 20
	idleTimeout      = 3 * time.Minute
	maxIDLength      = 128
	maxCandidateSize = 4 << 10
)

type peer struct {
	mu   sync.Mutex
	conn net.Conn
	enc  *json.Encoder
}

func (p *peer) send(m message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.enc.Encode(m)
}

type peerRoute struct{ id, remote string }

// Server is the rendezvous relay.
type Server struct {
	data       *dataRelay // nil: no data relay
	publicHost string
	version    string

	mu      sync.Mutex
	peers   map[peerRoute]*peer
	connsIP map[string]int
}

// NewServer returns a rendezvous server. data may be nil.
func NewServer(data *dataRelay, publicHost, version string) *Server {
	return &Server{data: data, publicHost: publicHost, version: version, peers: map[peerRoute]*peer{}, connsIP: map[string]int{}}
}

// Serve accepts connections until ln closes.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		ip := remoteIP(conn)
		s.mu.Lock()
		if s.connsIP[ip] >= maxConnsPerIP {
			s.mu.Unlock()
			conn.Close()
			continue
		}
		s.connsIP[ip]++
		s.mu.Unlock()
		go func() {
			defer func() {
				s.mu.Lock()
				if s.connsIP[ip]--; s.connsIP[ip] <= 0 {
					delete(s.connsIP, ip)
				}
				s.mu.Unlock()
			}()
			s.handle(conn, ip)
		}()
	}
}

func remoteIP(c net.Conn) string {
	host, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		return c.RemoteAddr().String()
	}
	return host
}

func (s *Server) handle(conn net.Conn, ip string) {
	defer conn.Close()
	self := &peer{conn: conn, enc: json.NewEncoder(conn)}
	reader := bufio.NewReaderSize(conn, 4096)

	var route peerRoute
	registered := false
	defer func() {
		if !registered {
			return
		}
		s.mu.Lock()
		if s.peers[route] == self {
			delete(s.peers, route)
		}
		s.mu.Unlock()
	}()

	tokens, last := float64(messagesPerSec), time.Now()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		line, err := readLine(reader)
		if err != nil {
			return
		}
		now := time.Now()
		tokens += now.Sub(last).Seconds() * messagesPerSec
		if tokens > messagesPerSec {
			tokens = messagesPerSec
		}
		last = now
		if tokens < 1 {
			_ = self.send(message{Type: "error", Message: "slow down"})
			continue
		}
		tokens--

		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			_ = self.send(message{Type: "error", Message: "malformed message"})
			return
		}
		switch m.Type {
		case "hello":
			if m.Pubkey == "" || len(m.Pubkey) > maxIDLength || len(m.To) > maxIDLength {
				_ = self.send(message{Type: "error", Message: "bad id"})
				return
			}
			s.mu.Lock()
			if registered && s.peers[route] == self {
				delete(s.peers, route)
			}
			route = peerRoute{id: m.Pubkey, remote: m.To}
			s.peers[route] = self
			registered = true
			s.mu.Unlock()

		case "offer":
			if !registered || len(m.Candidate) > maxCandidateSize {
				_ = self.send(message{Type: "error", Message: "send hello first, with a candidate under 4 KiB"})
				continue
			}
			s.mu.Lock()
			target, ok := s.peers[peerRoute{id: m.To, remote: route.id}]
			if !ok {
				target, ok = s.peers[peerRoute{id: m.To}]
			}
			s.mu.Unlock()
			if !ok {
				_ = self.send(message{Type: "error", Message: "peer not registered"})
				continue
			}
			if err := target.send(message{Type: "offer", From: route.id, Candidate: m.Candidate}); err != nil {
				_ = self.send(message{Type: "error", Message: "peer disconnected"})
				continue
			}
			_ = self.send(message{Type: "ack"})

		case "session":
			if s.data == nil {
				_ = self.send(message{Type: "error", Message: "this relay offers rendezvous only"})
				continue
			}
			sess, err := s.data.allocate(ip, m.Bytes, now)
			if err != nil {
				_ = self.send(message{Type: "error", Message: err.Error()})
				continue
			}
			_ = self.send(message{Type: "session", Session: &sessionGrant{
				ID:        sess.idHex(),
				Endpoint:  net.JoinHostPort(s.publicHost, strconv.Itoa(sess.port())),
				Secret:    hex.EncodeToString(sess.secret),
				Bytes:     sess.allotted,
				ExpiresAt: sess.expiresAt.Unix(),
			}})

		case "status":
			_ = self.send(message{Type: "status", Status: s.status(now)})

		default:
			_ = self.send(message{Type: "error", Message: fmt.Sprintf("unknown message type %q", m.Type)})
		}
	}
}

func (s *Server) status(now time.Time) *relayStatus {
	s.mu.Lock()
	peers := len(s.peers)
	s.mu.Unlock()
	st := &relayStatus{Version: s.version, Peers: peers, Free: true}
	if s.data != nil {
		st.Sessions = s.data.count()
		st.Budget = s.data.budget.status(now)
	}
	return st
}

// readLine reads one newline-terminated message of at most maxLineBytes.
func readLine(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if len(out) > maxLineBytes {
			return nil, fmt.Errorf("message over %d bytes", maxLineBytes)
		}
		if !isPrefix {
			return out, nil
		}
	}
}
