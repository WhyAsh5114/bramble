// Package wgnode wraps wireguard-go's Device with the minimal surface
// brambled needs: create a node, add/remove peers by public key, read the
// current peer set back. Peer admission itself is not implemented here —
// WireGuard's own protocol already refuses a handshake from any public key
// that isn't in the device's peer table (see package admission, which is
// what decides who gets added).
//
// Uses a netstack (gVisor) virtual TUN rather than a real OS TUN device, so
// nodes can run and be tested without root/CAP_NET_ADMIN — including in CI.
// Swapping in a real OS TUN for an actual cross-machine demo is an isolated
// later change: only the tun.Device passed to device.NewDevice would differ.
package wgnode

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// GenerateKeyPair produces a new WireGuard (Curve25519) key pair, hex-encoded.
// Real node identity eventually comes from enrollment (Ledger-backed private
// key storage, per docs/adr/0001) — this is for standing up a node ad hoc,
// e.g. in tests and the current minimal `brambled serve`.
func GenerateKeyPair() (privateHex, publicHex string, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", fmt.Errorf("generating private key: %w", err)
	}
	// Curve25519 clamping, per RFC 7748 / WireGuard's own key generation.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, &priv)

	return hex.EncodeToString(priv[:]), hex.EncodeToString(pub[:]), nil
}

type Node struct {
	dev  *device.Device
	tnet *netstack.Net
	cfg  Config
}

type Config struct {
	// PrivateKeyHex is the node's own WireGuard private key, hex-encoded.
	PrivateKeyHex string
	// ListenPort is the UDP port this node's outer transport binds to.
	ListenPort uint16
	// LocalAddress is this node's virtual address on its own netstack TUN.
	LocalAddress netip.Addr
}

func New(cfg Config) (*Node, error) {
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{cfg.LocalAddress}, nil, device.DefaultMTU)
	if err != nil {
		return nil, fmt.Errorf("creating virtual TUN: %w", err)
	}

	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))

	conf := fmt.Sprintf("private_key=%s\nlisten_port=%d\n", cfg.PrivateKeyHex, cfg.ListenPort)
	if err := dev.IpcSet(conf); err != nil {
		dev.Close()
		return nil, fmt.Errorf("configuring device: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("bringing device up: %w", err)
	}

	return &Node{dev: dev, tnet: tnet, cfg: cfg}, nil
}

// AddPeer authorizes publicKeyHex to complete a handshake with this node,
// routing allowedIP to it. endpoint is optional — omit it (zero value) for a
// peer that will dial in first; the device learns/updates the endpoint from
// the first valid handshake it receives (WireGuard's usual roaming
// behavior), which is why the admission loop doesn't need to know a peer's
// address to authorize it.
func (n *Node) AddPeer(publicKeyHex string, allowedIP netip.Prefix, endpoint *netip.AddrPort) error {
	var b strings.Builder
	fmt.Fprintf(&b, "public_key=%s\n", publicKeyHex)
	fmt.Fprintf(&b, "allowed_ip=%s\n", allowedIP.String())
	if endpoint != nil {
		fmt.Fprintf(&b, "endpoint=%s\n", endpoint.String())
	}
	return n.dev.IpcSet(b.String())
}

func (n *Node) RemovePeer(publicKeyHex string) error {
	conf := fmt.Sprintf("public_key=%s\nremove=true\n", publicKeyHex)
	return n.dev.IpcSet(conf)
}

type PeerInfo struct {
	PublicKeyHex        string
	AllowedIPs          []string
	LastHandshakeUnixNs int64
}

// Handshaked reports whether this peer has ever completed a handshake.
func (p PeerInfo) Handshaked() bool { return p.LastHandshakeUnixNs > 0 }

var publicKeyLine = regexp.MustCompile(`^public_key=([0-9a-f]{64})$`)

// Peers parses the device's current peer table from the UAPI "get" output
// (see golang.zx2c4.com/wireguard/device.Device.IpcGet — the config protocol
// documented at https://www.wireguard.com/xplatform/#configuration-protocol).
func (n *Node) Peers() ([]PeerInfo, error) {
	raw, err := n.dev.IpcGet()
	if err != nil {
		return nil, err
	}

	var peers []PeerInfo
	var cur *PeerInfo
	for _, line := range strings.Split(raw, "\n") {
		if m := publicKeyLine.FindStringSubmatch(line); m != nil {
			peers = append(peers, PeerInfo{PublicKeyHex: m[1]})
			cur = &peers[len(peers)-1]
			continue
		}
		if cur == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "allowed_ip":
			cur.AllowedIPs = append(cur.AllowedIPs, value)
		case "last_handshake_time_sec":
			secs, _ := strconv.ParseInt(value, 10, 64)
			cur.LastHandshakeUnixNs += secs * time.Second.Nanoseconds()
		case "last_handshake_time_nsec":
			nsecs, _ := strconv.ParseInt(value, 10, 64)
			cur.LastHandshakeUnixNs += nsecs
		}
	}
	return peers, nil
}

// Ping sends an ICMP echo to raddr over this node's tunnel and reports
// whether a reply was received within timeout. Used by tests to prove a
// handshake did or didn't succeed, the same signal Gate 0.2's manual test
// used across real networks.
func (n *Node) Ping(raddr netip.Addr, timeout time.Duration) (bool, error) {
	pc, err := n.tnet.DialPingAddr(n.cfg.LocalAddress, raddr)
	if err != nil {
		return false, err
	}
	defer pc.Close()

	if err := pc.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false, err
	}

	echo := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("bramble-gate1.1"),
		},
	}
	wireBytes, err := echo.Marshal(nil)
	if err != nil {
		return false, fmt.Errorf("marshaling ICMP echo: %w", err)
	}
	if _, err := pc.Write(wireBytes); err != nil {
		return false, err
	}

	buf := make([]byte, 1500)
	_, err = pc.Read(buf)
	if err != nil {
		return false, nil // timeout or refusal — not an error, just "no reply"
	}
	return true, nil
}

func (n *Node) Close() {
	n.dev.Close()
}
