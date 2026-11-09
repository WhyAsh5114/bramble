package wgnode

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/netip"

	"golang.zx2c4.com/wireguard/device"
)

func (n *Node) admittedPeer(publicKey string) (*device.Peer, error) {
	key, err := hex.DecodeString(publicKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("invalid peer public key")
	}
	var pk device.NoisePublicKey
	copy(pk[:], key)
	peer := n.dev.LookupPeer(pk)
	if peer == nil {
		return nil, fmt.Errorf("peer is no longer admitted")
	}
	return peer, nil
}

// SetPeerPath changes only an existing peer. update_only prevents a concurrent
// revocation from being undone by a late traversal result.
func (n *Node) SetPeerPath(publicKey string, endpoint netip.AddrPort) error {
	if _, err := n.admittedPeer(publicKey); err != nil {
		return err
	}
	if !endpoint.IsValid() || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" {
		return fmt.Errorf("invalid path endpoint")
	}
	return n.dev.IpcSet(fmt.Sprintf("public_key=%s\nupdate_only=true\nendpoint=%s\npersistent_keepalive_interval=1\n", publicKey, endpoint))
}

// InitiateHandshake asks WireGuard to authenticate the current path. WireGuard
// retains its normal retry/rate limits; no key material leaves the device.
func (n *Node) InitiateHandshake(publicKey string) error {
	peer, err := n.admittedPeer(publicKey)
	if err != nil {
		return err
	}
	return peer.SendHandshakeInitiation(false)
}

func (n *Node) Punch(ctx context.Context, publicKey string, endpoint netip.AddrPort) error {
	if _, err := n.admittedPeer(publicKey); err != nil {
		return err
	}
	if !n.cfg.EnableSTUN {
		return fmt.Errorf("shared discovery bind is disabled")
	}
	return n.stunBind.Punch(ctx, endpoint)
}
