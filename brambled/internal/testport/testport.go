// Package testport gives tests a UDP port that is free at the moment of the
// call. It replaces hard-coded WireGuard listen ports in brambled's tests.
//
// Hard-coded ports collide when more than one `go test` process runs on the
// same host. Found on 6 November 2026 (ETH-86): several agents ran brambled
// tests at once, and TestDataRelayFailover_SwitchesToBackupRelayWhenPrimaryStalls
// failed with `listen udp4 :61960: bind: address already in use` whenever
// another process already held 61960.
//
// Use wgnode.Config{ListenPort: 0} instead when the test never needs its own
// port number (for example, when every endpoint is a relay address). The
// kernel then picks the port and there is no race at all. Use FreeUDP when the
// test must hand its own address to a peer before the node starts.
package testport

import (
	"errors"
	"net"
	"sync"
	"syscall"
	"testing"
)

var (
	mu sync.Mutex
	// handedOut stops two calls in one test binary from returning the same
	// port, which the kernel could otherwise do once the first port is
	// released.
	handedOut = map[int]bool{}
)

// FreeUDP returns a port that is free on both udp4 and udp6, matching what
// wireguard-go's default bind opens for a given listen_port. The port is
// released before FreeUDP returns, so another process can still take it before
// the caller binds it. The kernel picks a random port from the ephemeral
// range, so that window is far smaller than for a fixed port shared by every
// checkout on the host. FreeUDP never returns the same port twice within one
// test binary.
func FreeUDP(t testing.TB) uint16 {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for attempt := 0; attempt < 50; attempt++ {
		v4, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			t.Fatalf("testport: listening on udp4: %v", err)
		}
		port := v4.LocalAddr().(*net.UDPAddr).Port
		if handedOut[port] {
			v4.Close()
			continue
		}

		v6, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6zero, Port: port})
		switch {
		case err == nil:
			v6.Close()
			v4.Close()
			handedOut[port] = true
			return uint16(port)
		case errors.Is(err, syscall.EAFNOSUPPORT):
			// No IPv6 on this host. wireguard-go tolerates that too.
			v4.Close()
			handedOut[port] = true
			return uint16(port)
		default:
			// Taken on udp6 only. Try another port.
			v4.Close()
		}
	}
	t.Fatal("testport: no UDP port free on both udp4 and udp6 after 50 attempts")
	return 0
}
