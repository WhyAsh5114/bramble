package relaysrv

import (
	"encoding/binary"
	"net"
	"sync"
	"time"
)

// ServeSTUN answers RFC 5389 Binding requests with XOR-MAPPED-ADDRESS, so
// nodes learn their public endpoint. A reply is 32 bytes for a 20-byte request
// (IPv4); a per-source rate limit keeps the relay from being a useful reflector.
func ServeSTUN(conn *net.UDPConn) error {
	const cookie = 0x2112A442
	const perSourcePerSecond = 20
	type bucket struct {
		n     int
		reset time.Time
	}
	var mu sync.Mutex
	buckets := map[string]*bucket{}
	buf := make([]byte, 1500)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		if n < 20 || binary.BigEndian.Uint16(buf[0:2]) != 0x0001 || binary.BigEndian.Uint32(buf[4:8]) != cookie {
			continue
		}
		now := time.Now()
		key := src.IP.String()
		mu.Lock()
		b := buckets[key]
		if b == nil || now.After(b.reset) {
			b = &bucket{reset: now.Add(time.Second)}
			buckets[key] = b
			if len(buckets) > 10_000 {
				for k, v := range buckets {
					if now.After(v.reset) {
						delete(buckets, k)
					}
				}
			}
		}
		b.n++
		over := b.n > perSourcePerSecond
		mu.Unlock()
		if over {
			continue
		}

		ip := src.IP.To4()
		family := byte(0x01)
		if ip == nil {
			ip = src.IP.To16()
			family = 0x02
		}
		attr := make([]byte, 4+len(ip))
		attr[1] = family
		binary.BigEndian.PutUint16(attr[2:4], uint16(src.Port)^uint16(cookie>>16))
		mask := make([]byte, 16)
		binary.BigEndian.PutUint32(mask[0:4], cookie)
		copy(mask[4:], buf[8:20])
		for i := range ip {
			attr[4+i] = ip[i] ^ mask[i]
		}
		resp := make([]byte, 20+4+len(attr))
		binary.BigEndian.PutUint16(resp[0:2], 0x0101)
		binary.BigEndian.PutUint16(resp[2:4], uint16(4+len(attr)))
		copy(resp[4:20], buf[4:20])
		binary.BigEndian.PutUint16(resp[20:22], 0x0020)
		binary.BigEndian.PutUint16(resp[22:24], uint16(len(attr)))
		copy(resp[24:], attr)
		_, _ = conn.WriteToUDP(resp, src)
	}
}
