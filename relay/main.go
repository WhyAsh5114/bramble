// Command relay is the dumb rendezvous relay Gate 0.2 requires
// (docs/10_DAY0_GATES.md:22): it forwards opaque candidate-exchange blobs
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
	"time"
)

// message is the wire protocol: newline-delimited JSON over TCP.
//
//	{"type":"hello","pubkey":"<hex>","token":"<rendezvous token, omitted if unmetered>"}  client -> relay
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
//
// Token is metering, not admission (docs/adr/0007) — a relay running with
// -meter requires one valid rendezvous token per hello (one per Exchange()
// call, not per offer retry, precisely because of the retry behavior
// described above); an unmetered relay ignores the field entirely.
type message struct {
	Type      string `json:"type"`
	Pubkey    string `json:"pubkey,omitempty"`
	To        string `json:"to,omitempty"`
	From      string `json:"from,omitempty"`
	Candidate string `json:"candidate,omitempty"`
	Message   string `json:"message,omitempty"`
	Token     string `json:"token,omitempty"`
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
// between them. It holds no notion of who is authorized to talk to whom —
// verifier, when set, only checks whether a connection attempt was paid
// for, a metering concern (docs/adr/0007), never an admission one.
type Server struct {
	mu       sync.Mutex
	peers    map[string]*peer
	verifier *tokenVerifier // nil means unmetered — every hello is accepted, as before
}

func NewServer() *Server {
	return &Server{peers: make(map[string]*peer)}
}

// NewMeteredServer is NewServer with rendezvous-token enforcement on every
// hello. secret must match whatever relay-sidecar was started with — see
// paySidecar in sidecar.go, which owns generating and distributing it.
func NewMeteredServer(secret []byte) *Server {
	return &Server{peers: make(map[string]*peer), verifier: newTokenVerifier(secret)}
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
			if s.verifier != nil {
				if err := s.verifier.verify(m.Token, time.Now()); err != nil {
					_ = self.send(message{Type: "error", Message: fmt.Sprintf("rendezvous payment required: %v", err)})
					return
				}
			}
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
	meter := flag.Bool("meter", false, "require a paid rendezvous token per hello (docs/adr/0007); off by default, matching every gate verified before this flag existed")
	payee := flag.String("payee", "", "Hedera account id to receive rendezvous fees (required with -meter)")
	sidecarDir := flag.String("sidecar-dir", "../relay-sidecar", "relay-sidecar project directory (required with -meter)")
	sidecarPort := flag.Int("sidecar-port", 7891, "local port relay-sidecar listens on")
	dataRelayFlag := flag.Bool("data-relay", false, "offer a bytes-metered data-plane relay (docs/adr/0008); requires -meter (payment lives in relay-sidecar too)")
	dataHost := flag.String("data-host", "0.0.0.0", "host interface data-relay session sockets bind to — needs to be reachable by both peers, unlike -addr's rendezvous listener")
	internalPort := flag.Int("internal-port", 7895, "loopback-only port relay-sidecar calls to allocate a data-relay session after payment settles")
	pricePerByte := flag.String("price-per-byte", "1", "atomic USDC price per byte forwarded, passed through to relay-sidecar's DynamicPrice (docs/adr/0008); ignored unless -data-relay is set")
	flag.Parse()

	if *dataRelayFlag && !*meter {
		fmt.Fprintln(os.Stderr, "error: -data-relay requires -meter (relay-sidecar handles both rendezvous and data-relay payment)")
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	log.Printf("rendezvous relay listening on %s", ln.Addr())

	srv := NewServer()
	if *meter {
		if *payee == "" {
			fmt.Fprintln(os.Stderr, "error: -payee is required with -meter")
			os.Exit(1)
		}
		var dr *dataRelay
		psConfig := paySidecarConfig{Dir: *sidecarDir, Port: *sidecarPort, Payee: *payee}
		if *dataRelayFlag {
			dr = newDataRelay()
			go func() {
				if err := serveInternalAPI(dr, *internalPort, *dataHost); err != nil {
					fmt.Fprintln(os.Stderr, "data-relay: internal API stopped:", err)
				}
			}()
			psConfig.DataRelay = true
			psConfig.InternalAPIURL = fmt.Sprintf("http://127.0.0.1:%d", *internalPort)
			psConfig.PricePerByte = *pricePerByte
		}

		fmt.Fprintln(os.Stderr, "starting relay-sidecar...")
		ps, err := startPaySidecar(psConfig)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		defer ps.Stop()
		srv = NewMeteredServer(ps.secret)
		log.Printf("metering enabled: rendezvous-token required, relay-sidecar on :%d", *sidecarPort)
		if *dataRelayFlag {
			log.Printf("data-relay enabled: sessions bind on %s, internal API on 127.0.0.1:%d", *dataHost, *internalPort)
		}
	}

	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
