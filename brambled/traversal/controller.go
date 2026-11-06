// Package traversal selects a path for an already admitted WireGuard peer.
// Rendezvous hints and UDP probes never add peers or grant access.
package traversal

import (
	"context"
	"net/netip"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// Controller runs for one admitted public key. Learn and Relay must honor ctx
// or bound their own I/O. Relay buys at most one session per controller; the
// production adapter elects one buyer by public-key ordering and caches it.
type Controller struct {
	Node             *wgnode.Node
	Peer             string
	Learn            func(context.Context) (rendezvous.Announcement, error)
	Relay            func(context.Context) (netip.AddrPort, error)
	OnState          func(string)
	HandshakeTimeout time.Duration
	RetryInterval    time.Duration
	StallTimeout     time.Duration
}

func (c *Controller) peer() (wgnode.PeerInfo, bool) {
	peers, err := c.Node.Peers()
	if err != nil {
		return wgnode.PeerInfo{}, false
	}
	for _, p := range peers {
		if p.PublicKeyHex == c.Peer {
			return p, true
		}
	}
	return wgnode.PeerInfo{}, false
}

func (c *Controller) Run(ctx context.Context) {
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = 8 * time.Second
	}
	if c.RetryInterval <= 0 {
		c.RetryInterval = 3 * time.Second
	}
	if c.StallTimeout <= 0 {
		c.StallTimeout = 15 * time.Second
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	hints := make(chan rendezvous.Announcement, 1)
	if c.Learn != nil {
		go func() {
			for ctx.Err() == nil {
				a, err := c.Learn(ctx)
				if err == nil {
					select {
					case hints <- a:
					case <-ctx.Done():
						return
					}
				}
				timer := time.NewTimer(c.RetryInterval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	var direct, relay, target netip.AddrPort
	state := ""
	emit := func(s string) {
		if state != s {
			state = s
			if c.OnState != nil {
				c.OnState(s)
			}
		}
	}
	emit("probing")
	deadline := time.Now().Add(c.HandshakeTimeout)
	var baselineHandshake, baselineRx int64
	var nextProbe, nextRelay time.Time
	lastRx := int64(-1)
	lastReceive := time.Now()
	fallback := func() {
		if !relay.IsValid() && c.Relay != nil && !time.Now().Before(nextRelay) {
			nextRelay = time.Now().Add(c.RetryInterval)
			ep, err := c.Relay(ctx)
			if err == nil {
				relay = ep
			}
		}
		if ctx.Err() != nil {
			return
		}
		if relay.IsValid() {
			if c.Node.SetPeerPath(c.Peer, relay) == nil {
				_ = c.Node.InitiateHandshake(c.Peer)
				emit("relay")
			}
		} else {
			emit("unavailable")
		}
		if nextProbe.IsZero() || state == "probing" || state == "direct" {
			nextProbe = time.Now().Add(c.RetryInterval)
		}
	}
	begin := func(p wgnode.PeerInfo) {
		target = direct
		baselineHandshake = p.LastHandshakeUnixNs
		baselineRx = p.RxBytes
		deadline = time.Now().Add(c.HandshakeTimeout)
		if c.Node.SetPeerPath(c.Peer, target) == nil {
			_ = c.Node.InitiateHandshake(c.Peer)
			emit("probing")
		}
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case a := <-hints:
			for _, ep := range a {
				if ep.Kind == rendezvous.KindRelay {
					relay = ep.Addr
				}
			}
			var candidates rendezvous.Announcement
			for _, ep := range a {
				if ep.Kind != rendezvous.KindRelay {
					candidates = append(candidates, ep)
				}
			}
			direct, _ = candidates.Best()
			if state == "unavailable" && relay.IsValid() {
				fallback()
			}
		case <-tick.C:
			p, ok := c.peer()
			if !ok {
				return
			}
			now := time.Now()
			switch state {
			case "probing":
				if !target.IsValid() && direct.IsValid() {
					begin(p)
				}
				if target.IsValid() && p.LastHandshakeUnixNs > baselineHandshake && p.RxBytes > baselineRx && p.Endpoint == target.String() {
					lastRx = p.RxBytes
					lastReceive = now
					emit("direct")
					continue
				}
				if now.After(deadline) {
					fallback()
					continue
				}
				if target.IsValid() {
					probeCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)
					_ = c.Node.Punch(probeCtx, c.Peer, target)
					stop()
					// An old relay packet may roam the endpoint during a direct attempt.
					_ = c.Node.SetPeerPath(c.Peer, target)
				}
			case "direct":
				if p.RxBytes != lastRx && p.Endpoint == target.String() {
					lastRx = p.RxBytes
					lastReceive = now
				}
				if now.Sub(lastReceive) >= c.StallTimeout {
					fallback()
				}
			case "relay", "unavailable":
				if state == "unavailable" && !now.Before(nextRelay) {
					fallback()
				}
				if direct.IsValid() && !now.Before(nextProbe) {
					nextProbe = now.Add(c.RetryInterval)
					probeCtx, stop := context.WithTimeout(ctx, 400*time.Millisecond)
					err := c.Node.Punch(probeCtx, c.Peer, direct)
					stop()
					if err == nil && ctx.Err() == nil {
						begin(p)
					}
				}
			}
		}
	}
}
