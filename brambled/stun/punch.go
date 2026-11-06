package stun

import (
	"context"
	"crypto/rand"
	"net"
	"net/netip"
	"time"

	"golang.zx2c4.com/wireguard/conn"
)

// Punch sends a nonce-bearing UDP probe on the WireGuard socket. A reply only
// proves UDP reachability, never identity or admission. The controller must
// still require an authenticated WireGuard handshake before using the path.
func (b *Bind) Punch(ctx context.Context, target netip.AddrPort) error {
	ep, err := b.ParseEndpoint(target.String())
	if err != nil {
		return err
	}
	var id transactionID
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	p := pending{server: ep.DstToString(), replies: make(chan result, 1)}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return net.ErrClosed
	}
	b.pending[id] = p
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.pending, id); b.mu.Unlock() }()
	packet := append([]byte("BRMPNCH0"), id[:]...)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := b.Send([][]byte{packet}, ep); err != nil {
			return err
		}
		select {
		case r := <-p.replies:
			return r.err
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (b *Bind) handlePunch(packet []byte, source conn.Endpoint) bool {
	if len(packet) != 20 || string(packet[:7]) != "BRMPNCH" {
		return false
	}
	if source == nil {
		return true
	}
	switch packet[7] {
	case '0':
		// Equal-size response: no amplification, no peer or endpoint mutation.
		reply := append([]byte(nil), packet...)
		reply[7] = '1'
		_ = b.Send([][]byte{reply}, source)
	case '1':
		var id transactionID
		copy(id[:], packet[8:])
		b.mu.Lock()
		defer b.mu.Unlock()
		if p, ok := b.pending[id]; ok && p.server == source.DstToString() {
			select {
			case p.replies <- result{}:
			default:
			}
		}
	}
	return true
}
