package brambled

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/gateway"
	"github.com/WhyAsh5114/bramble/brambled/statusapi"
	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// Handle lets an embedding program (the bramble daemon) use a running node.
type Handle struct {
	Label     string
	Address   netip.Addr
	PublicKey string

	node        *wgnode.Node
	tracker     *peerTracker
	status      *statusapi.Server
	gatewayPort uint16
	netstack    bool
}

// Peers returns every tracked peer with its admission and path state.
func (h *Handle) Peers() ([]statusapi.PeerStatus, error) { return h.status.Peers() }

// Activity returns recent admission and gateway decisions.
func (h *Handle) Activity() []statusapi.ActivityEvent { return h.status.Activity() }

// Endpoint reports this node's STUN-observed public endpoint.
func (h *Handle) Endpoint() wgnode.EndpointStatus { return h.node.EndpointStatus() }

func (h *Handle) dialGateway(peerLabel string) (net.Conn, error) {
	h.tracker.mu.Lock()
	p, ok := h.tracker.byLabel[peerLabel]
	h.tracker.mu.Unlock()
	if !ok || !p.AllowedIP.IsValid() {
		return nil, fmt.Errorf("%s is not a tracked peer", peerLabel)
	}
	target := netip.AddrPortFrom(p.AllowedIP.Addr(), h.gatewayPort)
	if !h.netstack {
		return net.DialTimeout("tcp", net.JoinHostPort(target.Addr().String(), strconv.Itoa(int(target.Port()))), dialTimeout)
	}
	// Netstack dials have no timeout of their own; a peer that revoked us
	// would hold the call for the TCP connect timeout (about 2 minutes).
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := h.node.DialTCP(target)
		if err != nil {
			ch <- result{nil, err} // DialTCP returns a typed nil on error
			return
		}
		ch <- result{c, nil}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-time.After(dialTimeout):
		go func() {
			if r := <-ch; r.c != nil {
				r.c.Close()
			}
		}()
		return nil, fmt.Errorf("%s did not answer within %s", peerLabel, dialTimeout)
	}
}

const dialTimeout = 5 * time.Second

// Services asks peerLabel's gateway which of its services this node may use.
func (h *Handle) Services(peerLabel string) ([]gateway.ServiceInfo, error) {
	conn, err := h.dialGateway(peerLabel)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return gateway.ListServices(conn)
}

// Dial opens a connection to service on peerLabel through its gateway.
func (h *Handle) Dial(peerLabel, service string) (net.Conn, error) {
	conn, err := h.dialGateway(peerLabel)
	if err != nil {
		return nil, err
	}
	c, err := gateway.Connect(conn, service)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}
