package relaysrv

import (
	"bufio"
	"errors"
	"net"
	"sync"
)

// peekConn reads through a bufio.Reader that already peeked at the first
// bytes, so nothing the protocol sniffing read is lost.
type peekConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// releaseConn runs release once when the connection closes, so the
// per-address connection count drops when an HTTP connection ends.
type releaseConn struct {
	*peekConn
	release func()
}

func (c *releaseConn) Close() error {
	defer c.release()
	return c.peekConn.Close()
}

// connQueue is a net.Listener fed by Serve, so net/http can serve the
// connections that opened with an HTTP request line.
type connQueue struct {
	addr net.Addr
	ch   chan net.Conn
	once sync.Once
	done chan struct{}
}

func newConnQueue(addr net.Addr) *connQueue {
	return &connQueue{addr: addr, ch: make(chan net.Conn, 16), done: make(chan struct{})}
}

func (q *connQueue) push(c net.Conn) {
	select {
	case q.ch <- c:
	case <-q.done:
		c.Close()
	}
}

func (q *connQueue) Accept() (net.Conn, error) {
	select {
	case c := <-q.ch:
		return c, nil
	case <-q.done:
		return nil, errors.New("listener closed")
	}
}

func (q *connQueue) Close() error {
	q.once.Do(func() { close(q.done) })
	return nil
}

func (q *connQueue) Addr() net.Addr { return q.addr }
