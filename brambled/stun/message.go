// Package stun discovers UDP mappings using unauthenticated RFC 5389 binding
// requests. A mapping is an observation, never peer admission authority.
package stun

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net/netip"
)

const cookie uint32 = 0x2112a442

type transactionID [12]byte

// bindingError is a validated server rejection, unlike malformed input.
type bindingError int

func (e bindingError) Error() string { return fmt.Sprintf("STUN binding error %d", int(e)) }

func request(id transactionID) []byte {
	b := make([]byte, 20)
	binary.BigEndian.PutUint16(b, 1)
	binary.BigEndian.PutUint32(b[4:], cookie)
	copy(b[8:], id[:])
	return b
}

func isSTUN(b []byte) bool {
	return len(b) >= 20 && b[0]&0xc0 == 0 && binary.BigEndian.Uint32(b[4:]) == cookie
}

func response(b []byte) (netip.AddrPort, error) {
	if !isSTUN(b) || int(binary.BigEndian.Uint16(b[2:])) != len(b)-20 || len(b)%4 != 0 {
		return netip.AddrPort{}, fmt.Errorf("invalid STUN framing")
	}
	kind := binary.BigEndian.Uint16(b)
	if kind != 0x101 && kind != 0x111 {
		return netip.AddrPort{}, fmt.Errorf("not a binding response")
	}
	var mapped, xorMapped netip.AddrPort
	var errorCode int
	for pos := 20; pos < len(b); {
		if len(b)-pos < 4 {
			return netip.AddrPort{}, fmt.Errorf("truncated attribute")
		}
		typ, size := binary.BigEndian.Uint16(b[pos:]), int(binary.BigEndian.Uint16(b[pos+2:]))
		end := pos + 4 + size
		next := pos + 4 + ((size + 3) &^ 3)
		if end > len(b) || next > len(b) {
			return netip.AddrPort{}, fmt.Errorf("truncated attribute value")
		}
		v := b[pos+4 : end]
		switch typ {
		case 0x0001, 0x0020:
			ap, err := address(v, typ == 0x0020, b[4:20])
			if err != nil {
				return netip.AddrPort{}, err
			}
			if typ == 0x0020 {
				if !xorMapped.IsValid() {
					xorMapped = ap
				}
			} else if !mapped.IsValid() {
				mapped = ap
			}
		case 0x0009:
			if len(v) < 4 || v[2]&7 < 3 || v[2]&7 > 6 || v[3] > 99 {
				return netip.AddrPort{}, fmt.Errorf("invalid error code")
			}
			errorCode = int(v[2]&7)*100 + int(v[3])
		case 0x8028:
			if size != 4 || next != len(b) || binary.BigEndian.Uint32(v) != crc32.ChecksumIEEE(b[:pos])^0x5354554e {
				return netip.AddrPort{}, fmt.Errorf("invalid fingerprint")
			}
		case 0x8022, 0x8023: // SOFTWARE, ALTERNATE-SERVER: never follow redirects.
		default:
			if typ < 0x8000 {
				return netip.AddrPort{}, fmt.Errorf("unsupported required attribute")
			}
		}
		pos = next
	}
	if kind == 0x111 {
		if errorCode == 0 {
			return netip.AddrPort{}, fmt.Errorf("missing error code")
		}
		return netip.AddrPort{}, bindingError(errorCode)
	}
	if xorMapped.IsValid() {
		return xorMapped, nil
	}
	if mapped.IsValid() {
		return mapped, nil
	}
	return netip.AddrPort{}, fmt.Errorf("binding response has no mapped address")
}

func address(v []byte, xor bool, mask []byte) (netip.AddrPort, error) {
	if len(v) < 4 {
		return netip.AddrPort{}, fmt.Errorf("short mapped address")
	}
	size := 4
	if v[1] == 2 {
		size = 16
	} else if v[1] != 1 {
		return netip.AddrPort{}, fmt.Errorf("unknown address family")
	}
	if len(v) != size+4 {
		return netip.AddrPort{}, fmt.Errorf("invalid address length")
	}
	port := binary.BigEndian.Uint16(v[2:])
	raw := append([]byte(nil), v[4:]...)
	if xor {
		port ^= uint16(cookie >> 16)
		for i := range raw {
			raw[i] ^= mask[i]
		}
	}
	ip, _ := netip.AddrFromSlice(raw)
	if port == 0 || ip.IsUnspecified() || ip.IsMulticast() {
		return netip.AddrPort{}, fmt.Errorf("unusable mapped address")
	}
	return netip.AddrPortFrom(ip.Unmap(), port), nil
}
