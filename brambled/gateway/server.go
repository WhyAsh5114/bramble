package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"strings"
	"time"
)

// maxRequestLineLen bounds the CONNECT line read — this listener parses one
// short line, never an arbitrary stream, before deciding to proxy or deny.
const maxRequestLineLen = 256

// connectLineDeadline bounds how long a peer has to send its CONNECT line
// before the connection is dropped — prevents a slow/silent peer from
// holding a listener slot open indefinitely.
const connectLineDeadline = 5 * time.Second

// Server is the gateway's single fixed-port CONNECT listener (adr/0005 §
// "What Section C still owns", adr/0006). It never trusts a self-declared
// caller identity — the requesting label is looked up from the already
// WireGuard-authenticated tunnel source IP, never read from the request
// itself (see PeerLabels).
type Server struct {
	// Listener is the already-bound listener for this node's tunnel
	// interface — a plain net.Listener for a real OS TUN node (any address
	// on a kernel-visible TUN interface works with the standard library, no
	// wgnode changes needed), or the result of wgnode.Node.ListenTCP in
	// netstack mode. Server does not create or own binding logic itself.
	Listener net.Listener
	// Resolver reads on-chain device state (acl, acl-granters, pubkey).
	Resolver Resolver
	// PrivateKeyHex is this node's own WireGuard private key — reused
	// directly as the ECDH identity key (adr/0005: "WG keys are the PKI
	// substrate"), no separate secret.
	PrivateKeyHex string
	// OwnLabel is this node's own ENS label, used to resolve its own
	// acl-granters record.
	OwnLabel string
	// Services maps a symbolic, never-published service name to a local
	// port this node forwards an authorized CONNECT to. Purely local
	// config — never read from or written to chain state (adr/0005 Q1).
	Services map[string]uint16
	// PeerLabels maps a peer's tunnel address to its ENS label, built from
	// the same static -peer flags admission.Loop already tracks (see
	// adr/0006's correction to adr/0005's speculative pubkey-reverse-lookup
	// note) — WireGuard's own allowed_ip binding makes this mapping
	// trustworthy for an already-decrypted inbound connection.
	PeerLabels map[netip.Addr]string

	// Logf receives one line per accepted/denied/proxied connection.
	// Defaults to log.Printf if unset.
	Logf func(format string, args ...any)

	// OnDecision, if set, is called once per CONNECT attempt alongside the
	// Logf call at the same point — a structured counterpart to the log
	// line, for a caller that wants to keep a queryable feed of recent
	// gateway activity (e.g. brambled/statusapi) without scraping log text.
	OnDecision func(Decision)
}

// Decision reports the outcome of one CONNECT attempt handled by Server.
type Decision struct {
	RequesterLabel string
	Service        string
	Allowed        bool
	Detail         string
}

func (s *Server) emit(d Decision) {
	if s.OnDecision != nil {
		s.OnDecision(d)
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Serve accepts connections until ctx is cancelled or the listener errors.
// Each connection is handled in its own goroutine — a slow or malicious peer
// on one connection never blocks admission of another.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.Listener.Close()
	}()

	for {
		conn, err := s.Listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("accepting connection: %w", err)
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	requesterLabel, service, reader, err := s.readRequest(conn)
	if err != nil {
		s.logf("gateway: rejecting connection from %s: %v", conn.RemoteAddr(), err)
		return
	}

	port, known := s.Services[service]
	if !known {
		s.logf("gateway: %s requested unknown service %q — denied", requesterLabel, service)
		s.emit(Decision{RequesterLabel: requesterLabel, Service: service, Allowed: false, Detail: "unknown service"})
		fmt.Fprintf(conn, "DENY\n")
		return
	}

	allowed, err := CheckACL(s.Resolver, s.PrivateKeyHex, s.OwnLabel, requesterLabel, service)
	if err != nil {
		s.logf("gateway: ACL check failed for %s requesting %q: %v", requesterLabel, service, err)
		s.emit(Decision{RequesterLabel: requesterLabel, Service: service, Allowed: false, Detail: "ACL check failed: " + err.Error()})
		fmt.Fprintf(conn, "DENY\n")
		return
	}
	if !allowed {
		s.logf("gateway: %s denied for service %q (no matching granted digest)", requesterLabel, service)
		s.emit(Decision{RequesterLabel: requesterLabel, Service: service, Allowed: false, Detail: "no matching granted digest"})
		fmt.Fprintf(conn, "DENY\n")
		return
	}

	backend, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		s.logf("gateway: %s allowed for %q but local service unreachable: %v", requesterLabel, service, err)
		s.emit(Decision{RequesterLabel: requesterLabel, Service: service, Allowed: false, Detail: "local service unreachable: " + err.Error()})
		fmt.Fprintf(conn, "DENY\n")
		return
	}
	defer backend.Close()

	if _, err := fmt.Fprintf(conn, "OK\n"); err != nil {
		return
	}
	s.logf("gateway: %s allowed for %q — proxying to 127.0.0.1:%d", requesterLabel, service, port)
	s.emit(Decision{RequesterLabel: requesterLabel, Service: service, Allowed: true, Detail: fmt.Sprintf("proxying to 127.0.0.1:%d", port)})
	// reader, not conn, is the read side from here — see readRequest's doc
	// comment on why using the raw conn here would silently drop bytes.
	proxy(bufferedConn{reader: reader, Conn: conn}, backend)
}

// readRequest parses exactly one "CONNECT <service>\n" line and resolves the
// caller's label from its tunnel source IP — never from the request itself.
// It returns the bufio.Reader it used, not just the parsed fields: a
// client's first write can legitimately contain more than the CONNECT line
// (pipelined payload bytes in the same TCP segment, saving a round trip) —
// bufio's fill() may have already buffered those bytes internally.
// Discarding this reader and proxying from the raw conn instead would
// silently drop them, corrupting the stream. Callers must proxy through the
// returned reader, not conn, for the read direction.
func (s *Server) readRequest(conn net.Conn) (requesterLabel, service string, reader *bufio.Reader, err error) {
	remoteAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return "", "", nil, fmt.Errorf("unexpected remote address type %T", conn.RemoteAddr())
	}
	label, known := s.PeerLabels[remoteAddr.AddrPort().Addr()]
	if !known {
		return "", "", nil, fmt.Errorf("no tracked peer for tunnel address %s", remoteAddr.AddrPort().Addr())
	}

	if err := conn.SetReadDeadline(time.Now().Add(connectLineDeadline)); err != nil {
		return "", "", nil, fmt.Errorf("setting read deadline: %w", err)
	}
	reader = bufio.NewReaderSize(conn, maxRequestLineLen)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", "", nil, fmt.Errorf("reading request line: %w", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return "", "", nil, fmt.Errorf("clearing read deadline: %w", err)
	}

	line = strings.TrimSpace(line)
	prefix, service, ok := strings.Cut(line, " ")
	if !ok || prefix != "CONNECT" || service == "" {
		return "", "", nil, fmt.Errorf("malformed request %q, expected \"CONNECT <service>\"", line)
	}

	return label, service, reader, nil
}

// bufferedConn lets an already-partially-read net.Conn keep working with
// code that expects a plain reader/writer pair: reads come from reader
// (which may already hold bytes read past a parsed line — see
// Server.readRequest's doc comment), writes/close/deadlines still go
// straight to the embedded Conn. net.Conn already declares its own Read,
// which would otherwise make embedding both an ambiguous selector — Read is
// overridden explicitly instead of relying on embedding to pick one.
type bufferedConn struct {
	reader io.Reader
	net.Conn
}

func (b bufferedConn) Read(p []byte) (int, error) { return b.reader.Read(p) }

// proxy copies bytes bidirectionally until either side closes.
func proxy(a, b io.ReadWriter) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
