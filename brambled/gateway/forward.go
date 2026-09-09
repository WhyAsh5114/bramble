package gateway

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

// Forwarder is the client-side counterpart to Server: a local TCP listener
// that speaks nothing but plain TCP to whatever connects to it — a real
// psql, an agent's HTTP client, curl, anything unaware this proxy exists.
// Per accepted local connection, it dials the target gateway over the
// tunnel, injects the CONNECT preamble on the caller's behalf, and proxies
// raw bytes on success. Without this, nothing that doesn't already know
// about the gateway's CONNECT protocol could use it (adr/0006).
type Forwarder struct {
	// Listener is the local listener unmodified clients connect to, e.g.
	// 127.0.0.1:5433. Forwarder does not create or own binding logic.
	Listener net.Listener
	// DialTunnel opens one fresh connection to the target gateway's fixed
	// CONNECT port, over the mesh tunnel — a plain net.Dial to the peer's
	// tunnel address for a real OS TUN node, or wgnode.Node.DialTCP in
	// netstack mode. Called once per accepted local connection.
	DialTunnel func() (net.Conn, error)
	// Service is the symbolic name sent in the CONNECT request.
	Service string

	// Logf receives one line per forwarded/denied connection. Defaults to
	// log.Printf if unset.
	Logf func(format string, args ...any)
}

func (f *Forwarder) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Serve accepts local connections until ctx is cancelled or the listener
// errors. Each connection is handled in its own goroutine.
func (f *Forwarder) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		f.Listener.Close()
	}()

	for {
		local, err := f.Listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("accepting local connection: %w", err)
		}
		go f.handle(local)
	}
}

func (f *Forwarder) handle(local net.Conn) {
	defer local.Close()

	tunnel, err := f.DialTunnel()
	if err != nil {
		f.logf("forward: %s: dialing gateway for %q failed: %v", local.RemoteAddr(), f.Service, err)
		return
	}
	defer tunnel.Close()

	if _, err := fmt.Fprintf(tunnel, "CONNECT %s\n", f.Service); err != nil {
		f.logf("forward: %s: writing CONNECT for %q failed: %v", local.RemoteAddr(), f.Service, err)
		return
	}

	if err := tunnel.SetReadDeadline(time.Now().Add(connectLineDeadline)); err != nil {
		f.logf("forward: %s: setting read deadline failed: %v", local.RemoteAddr(), err)
		return
	}
	reader := bufio.NewReaderSize(tunnel, maxRequestLineLen)
	status, err := reader.ReadString('\n')
	if err != nil {
		f.logf("forward: %s: reading gateway response for %q failed: %v", local.RemoteAddr(), f.Service, err)
		return
	}
	if err := tunnel.SetReadDeadline(time.Time{}); err != nil {
		f.logf("forward: %s: clearing read deadline failed: %v", local.RemoteAddr(), err)
		return
	}

	if strings.TrimSpace(status) != "OK" {
		// The gateway denied it (DENY, or anything else) — the local caller
		// just sees the connection close, same as if the port refused it.
		f.logf("forward: %s: %q denied by gateway", local.RemoteAddr(), f.Service)
		return
	}

	f.logf("forward: %s: %q allowed — proxying", local.RemoteAddr(), f.Service)
	// reader, not tunnel, is the read side from here — same reason as
	// Server.handle: the gateway's OK line and any immediately-following
	// backend bytes can arrive in the same read, and bufio may have already
	// buffered past the line.
	proxy(local, bufferedConn{reader: reader, Conn: tunnel})
}
