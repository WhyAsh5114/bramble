// Command relay is the dumb rendezvous relay Gate 0.2 requires
// (docs/11_DAY0_GATES.md:22): it forwards opaque candidate-exchange blobs
// between two node agents identified by their WireGuard public key. It never
// decides who may talk to whom — that decision is made independently by each
// peer's own admission verifier against ENS state (see
// brambled/admission). A relay that lied about a candidate could only ever
// cause a doomed handshake attempt, never an admitted one (docs/adr/0003,
// "Trust boundary") — which is why there is deliberately no authorization
// logic anywhere in this file.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
)

// message is the wire protocol: newline-delimited JSON over TCP.
//
//	{"type":"hello","pubkey":"<hex>"}                                  client -> relay
//	{"type":"offer","to":"<peer pubkey>","candidate":"<ip:port>"}      client -> relay
//	{"type":"offer","from":"<sender pubkey>","candidate":"<ip:port>"}  relay -> client
//	{"type":"ack"}                                                     relay -> client (offer was forwarded)
//	{"type":"error","message":"..."}                                   relay -> client
//
// The ack matters because two peers rarely call in at exactly the same
// instant: whichever calls first gets an error (its target isn't registered
// yet) and must retry. Without an ack telling the sender its offer eventually
// landed, a client that stops as soon as it receives the *other* side's offer
// can vanish mid-retry, leaving its own never-delivered — see
// brambled/rendezvous's client, which waits for both.
type message struct {
	Type      string `json:"type"`
	Pubkey    string `json:"pubkey,omitempty"`
	To        string `json:"to,omitempty"`
	From      string `json:"from,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	Message   string `json:"message,omitempty"`
}

// peer is one registered connection. Writes are serialized because an offer
// forwarded to this peer (from another peer's goroutine) can race this
// peer's own outgoing error replies.
type peer struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (p *peer) send(m message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enc.Encode(m)
}

// Server pairs registered peers by public key and forwards opaque offers
// between them. It holds no notion of who is authorized to talk to whom.
type Server struct {
	mu    sync.Mutex
	peers map[string]*peer
}

func NewServer() *Server {
	return &Server{peers: make(map[string]*peer)}
}

func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	self := &peer{enc: json.NewEncoder(conn)}
	dec := json.NewDecoder(conn)

	var pubkey string
	defer func() {
		if pubkey == "" {
			return
		}
		s.mu.Lock()
		if s.peers[pubkey] == self {
			delete(s.peers, pubkey)
		}
		s.mu.Unlock()
	}()

	for {
		var m message
		if err := dec.Decode(&m); err != nil {
			return
		}

		switch m.Type {
		case "hello":
			pubkey = m.Pubkey
			s.mu.Lock()
			s.peers[pubkey] = self
			s.mu.Unlock()

		case "offer":
			s.mu.Lock()
			target, ok := s.peers[m.To]
			s.mu.Unlock()
			if !ok {
				_ = self.send(message{Type: "error", Message: fmt.Sprintf("peer %q not registered", m.To)})
				continue
			}
			_ = target.send(message{Type: "offer", From: pubkey, Candidate: m.Candidate})
			_ = self.send(message{Type: "ack"})

		default:
			_ = self.send(message{Type: "error", Message: fmt.Sprintf("unknown message type %q", m.Type)})
		}
	}
}

func main() {
	addr := flag.String("addr", ":9420", "TCP address to listen on")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	log.Printf("rendezvous relay listening on %s", ln.Addr())

	if err := NewServer().Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
