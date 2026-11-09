package relaysrv

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"time"
)

// A data-relay session forwards only between sources that proved the session
// secret. Each peer sends a bind packet from its WireGuard socket:
//
//	"BRBIND1" | side (1 byte, '0' or '1') | session id (8) | unix seconds (8) | mac (16)
//
// mac = HMAC-SHA256(secret, "bramble-relay-bind-v1" | side | session id | unix seconds)[:16].
// The relay binds the packet's source address to that side. Bind packets are
// never forwarded. A bind older than bindWindow is ignored, which limits a
// replay to an attacker already on the path, who could drop the traffic anyway.
// brambled/relaybind holds the client copy of this format.

const (
	bindMagic  = "BRBIND1"
	bindLen    = 7 + 1 + 8 + 8 + 16
	bindWindow = 2 * time.Minute
)

func bindMAC(secret []byte, side byte, id [8]byte, ts uint64) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("bramble-relay-bind-v1"))
	m.Write([]byte{side})
	m.Write(id[:])
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], ts)
	m.Write(t[:])
	return m.Sum(nil)[:16]
}

// parseBind returns the side a valid bind packet claims. ok is false for any
// other packet, including a bind for another session or with a bad mac.
func parseBind(packet []byte, id [8]byte, secret []byte, now time.Time) (side int, isBind, ok bool) {
	if len(packet) != bindLen || string(packet[:7]) != bindMagic {
		return 0, false, false
	}
	s := packet[7]
	if s != '0' && s != '1' {
		return 0, true, false
	}
	var got [8]byte
	copy(got[:], packet[8:16])
	if got != id {
		return 0, true, false
	}
	ts := binary.BigEndian.Uint64(packet[16:24])
	age := now.Sub(time.Unix(int64(ts), 0))
	if age > bindWindow || age < -bindWindow {
		return 0, true, false
	}
	if !hmac.Equal(packet[24:40], bindMAC(secret, s, id, ts)) {
		return 0, true, false
	}
	return int(s - '0'), true, true
}

// MakeBind builds a bind packet; the relay tests use it as the client would.
func MakeBind(secret []byte, side byte, id [8]byte, now time.Time) []byte {
	ts := uint64(now.Unix())
	out := make([]byte, 0, bindLen)
	out = append(out, bindMagic...)
	out = append(out, side)
	out = append(out, id[:]...)
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], ts)
	out = append(out, t[:]...)
	return append(out, bindMAC(secret, side, id, ts)...)
}
