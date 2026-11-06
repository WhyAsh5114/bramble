package rendezvous

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// Endpoint exchange (W8b). A node publishes the UDP endpoints it can be
// reached at — typically one learned from STUN — as the opaque candidate
// string Exchange already carries. The wire format is specified in
// docs/reference/endpoint-exchange.md. An endpoint learned this way is a
// hint for where to send probes. It never grants admission: that decision
// comes only from ENS (docs/15_MUMBAI_HANDOFF.md, invariant 1).

// EndpointKind says how a node learned an endpoint.
type EndpointKind string

const (
	// KindStatic is an operator-supplied address (-my-candidate). It is also
	// what a legacy bare "ip:port" candidate decodes to.
	KindStatic EndpointKind = "static"
	// KindSTUN is a server-reflexive address a STUN server reported.
	KindSTUN EndpointKind = "srflx"
	// KindHost is a local interface address, useful only on a shared LAN.
	KindHost EndpointKind = "host"
	// KindRelay identifies a shared data-relay session.
	KindRelay EndpointKind = "relay"
)

const (
	announcementPrefix = "ep1:"

	// MaxEndpoints and MaxAnnouncementLen bound what a hostile rendezvous
	// relay can make a node parse.
	MaxEndpoints       = 8
	MaxAnnouncementLen = 512
)

// ErrNotAdmitted reports that ENS has not admitted the peer, so the node
// neither published its endpoints to the peer nor accepted any from it.
var ErrNotAdmitted = errors.New("peer is not admitted by ENS")

// Endpoint is one reachable UDP address and how it was learned.
type Endpoint struct {
	Kind EndpointKind
	Addr netip.AddrPort
}

// Announcement is the ordered endpoint list one node publishes.
type Announcement []Endpoint

// kindPreference orders endpoints for callers that can try only one:
// operator intent first, then STUN, then LAN-only addresses.
var kindPreference = []EndpointKind{KindStatic, KindSTUN, KindHost, KindRelay}

func knownKind(k EndpointKind) bool {
	for _, known := range kindPreference {
		if k == known {
			return true
		}
	}
	return false
}

// Encode renders a as a candidate string. It rejects an empty list, an
// unknown kind, an unusable address, or a result over the size limits, so a
// node never publishes something its peers would refuse. Duplicate
// addresses collapse to the first occurrence.
func (a Announcement) Encode() (string, error) {
	if len(a) == 0 {
		return "", errors.New("no endpoints to announce")
	}
	seen := make(map[netip.AddrPort]struct{}, len(a))
	parts := make([]string, 0, len(a))
	for _, ep := range a {
		if !knownKind(ep.Kind) {
			return "", fmt.Errorf("unknown endpoint kind %q", ep.Kind)
		}
		if err := checkAddr(ep.Addr); err != nil {
			return "", fmt.Errorf("endpoint %s: %w", ep.Addr, err)
		}
		if _, dup := seen[ep.Addr]; dup {
			continue
		}
		seen[ep.Addr] = struct{}{}
		parts = append(parts, string(ep.Kind)+"="+ep.Addr.String())
	}
	if len(parts) > MaxEndpoints {
		return "", fmt.Errorf("%d endpoints exceeds the limit of %d", len(parts), MaxEndpoints)
	}
	out := announcementPrefix + strings.Join(parts, ",")
	if len(out) > MaxAnnouncementLen {
		return "", fmt.Errorf("announcement is %d bytes, limit is %d", len(out), MaxAnnouncementLen)
	}
	return out, nil
}

// ParseAnnouncement decodes a candidate string received from the
// rendezvous relay. It accepts the "ep1:" format and, for peers that have
// not upgraded, a bare "ip:port". The empty string means the peer published
// no address and yields an empty Announcement. Entries of an unknown kind
// are skipped so the format can grow; a malformed or unusable address
// fails the whole message. ParseAnnouncement does not decide whether the
// peer is trusted; see Exchanger.Learn.
func ParseAnnouncement(candidate string) (Announcement, error) {
	if candidate == "" {
		return nil, nil
	}
	if len(candidate) > MaxAnnouncementLen {
		return nil, fmt.Errorf("announcement is %d bytes, limit is %d", len(candidate), MaxAnnouncementLen)
	}
	if !strings.HasPrefix(candidate, announcementPrefix) {
		addr, err := netip.ParseAddrPort(candidate)
		if err != nil {
			return nil, fmt.Errorf("candidate %q is neither an %q announcement nor ip:port: %w", candidate, announcementPrefix, err)
		}
		if err := checkAddr(addr); err != nil {
			return nil, fmt.Errorf("endpoint %s: %w", addr, err)
		}
		return Announcement{{Kind: KindStatic, Addr: addr}}, nil
	}

	entries := strings.Split(strings.TrimPrefix(candidate, announcementPrefix), ",")
	if len(entries) > MaxEndpoints {
		return nil, fmt.Errorf("%d endpoints exceeds the limit of %d", len(entries), MaxEndpoints)
	}
	var out Announcement
	seen := make(map[netip.AddrPort]struct{}, len(entries))
	for _, entry := range entries {
		kind, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("entry %q is not kind=ip:port", entry)
		}
		addr, err := netip.ParseAddrPort(value)
		if err != nil {
			return nil, fmt.Errorf("entry %q: %w", entry, err)
		}
		if err := checkAddr(addr); err != nil {
			return nil, fmt.Errorf("endpoint %s: %w", addr, err)
		}
		if !knownKind(EndpointKind(kind)) {
			continue
		}
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, Endpoint{Kind: EndpointKind(kind), Addr: addr})
	}
	return out, nil
}

// checkAddr rejects addresses that are never a valid probe target.
// Loopback is checked separately by the learner because tests need it.
// A zone is rejected because it is free text from the relay: it can carry
// a newline, and the endpoint is later written into a WireGuard UAPI line.
func checkAddr(a netip.AddrPort) error {
	ip := a.Addr().Unmap()
	switch {
	case !a.IsValid():
		return errors.New("invalid address")
	case a.Addr().Zone() != "":
		return errors.New("address has an IPv6 zone")
	case a.Port() == 0:
		return errors.New("port 0")
	case ip.IsUnspecified():
		return errors.New("unspecified address")
	case ip.IsMulticast():
		return errors.New("multicast address")
	case ip.IsLinkLocalUnicast():
		return errors.New("link-local address")
	case ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}):
		return errors.New("broadcast address")
	}
	return nil
}

// Best returns the endpoint a caller that can try only one should use, or
// false if the announcement is empty.
func (a Announcement) Best() (netip.AddrPort, bool) {
	for _, kind := range kindPreference {
		for _, ep := range a {
			if ep.Kind == kind {
				return ep.Addr, true
			}
		}
	}
	return netip.AddrPort{}, false
}

// ExchangeFunc performs one rendezvous exchange: it publishes
// myCandidate for peerPubkey and returns the candidate peerPubkey
// published in return. The production implementation wraps Exchange with
// relay selection and the paid token (brambled/main.go's exchangeAnyMetered).
type ExchangeFunc func(ownPubkey, peerPubkey, myCandidate string) (peerCandidate string, err error)

// Exchanger learns a peer's endpoints through the paid rendezvous channel.
type Exchanger struct {
	// Own is this node's WireGuard public key, hex.
	Own string
	// Local returns the endpoints to publish. It is a function so a STUN
	// refresh is picked up on the next exchange.
	Local func() Announcement
	// Via runs the rendezvous exchange.
	Via ExchangeFunc
	// Admitted reports whether ENS currently admits the peer's public key.
	// Required: a nil Admitted refuses every peer.
	Admitted func(peerPubkey string) bool
	// AllowLoopback accepts loopback endpoints from peers. Only tests that
	// run both nodes on one host set it.
	AllowLoopback bool
}

// Learn exchanges endpoints with peerPubkey and returns the usable ones.
//
// Learn touches the rendezvous channel only for a peer ENS admits, so a
// non-admitted peer neither costs a rendezvous payment nor sees this node's
// endpoints. It checks admission again after the exchange, because a
// revocation can land while the exchange waits for the peer; endpoints from
// a peer revoked meanwhile are discarded.
func (e *Exchanger) Learn(peerPubkey string) (Announcement, error) {
	if e.Admitted == nil || !e.Admitted(peerPubkey) {
		return nil, ErrNotAdmitted
	}

	var mine string
	if e.Local != nil {
		if local := e.Local(); len(local) > 0 {
			encoded, err := local.Encode()
			if err != nil {
				return nil, fmt.Errorf("encoding own endpoints: %w", err)
			}
			mine = encoded
		}
	}

	theirs, err := e.Via(e.Own, peerPubkey, mine)
	if err != nil {
		return nil, err
	}

	if !e.Admitted(peerPubkey) {
		return nil, ErrNotAdmitted
	}

	learned, err := ParseAnnouncement(theirs)
	if err != nil {
		return nil, fmt.Errorf("peer %s published an unusable announcement: %w", peerPubkey, err)
	}
	usable := learned[:0:0]
	for _, ep := range learned {
		if ep.Addr.Addr().IsLoopback() && !e.AllowLoopback {
			continue
		}
		usable = append(usable, ep)
	}
	return usable, nil
}

// ViaRelay adapts Exchange against one relay address to an ExchangeFunc,
// for callers with a single unmetered relay (tests, the demo's static
// relay). token is presented in hello; pass "" for an unmetered relay.
func ViaRelay(addr, token string, timeout time.Duration) ExchangeFunc {
	return func(ownPubkey, peerPubkey, myCandidate string) (string, error) {
		return Exchange(addr, ownPubkey, peerPubkey, myCandidate, token, timeout)
	}
}
