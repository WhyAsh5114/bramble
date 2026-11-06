# How to test UDP traversal with simulated NATs

Run these commands from the repository root on Linux with Go 1.27 installed:

```sh
cd brambled
go test -tags nattest ./internal/nattest -v
go test -race -tags nattest ./internal/nattest -count=20
```

`pnpm verify` also runs these self-tests through the `brambled` package test
script. Regular `go build` and `go test` exclude this fixture. No root, TUN
device, network namespace, credentials, or internet connection is required
after dependencies are installed. All real sockets bind to loopback IPv4.

To use the fixture in another `brambled` integration test:

1. Add `//go:build nattest` as the first line of the test file.
2. Import `github.com/WhyAsh5114/bramble/brambled/internal/nattest`.
3. Create a NAT and register cleanup:

   ```go
   router, err := nattest.New(nattest.RestrictedCone, net.ParseIP("127.0.0.2"))
   if err != nil {
       t.Fatal(err)
   }
   t.Cleanup(func() { router.Close() })
   socket, err := router.ListenPacket()
   if err != nil {
       t.Fatal(err)
   }
   ```

4. Give the returned `net.PacketConn` to the UDP transport under test. Send
   discovery and probe packets through this same connection. Its `LocalAddr`
   is a synthetic private address, not a public candidate. Learn its public
   mapping from the source address observed by a loopback discovery server.
   The fixture forwards arbitrary bytes; tests supply the STUN server or codec.
5. Use a distinct loopback IP for every NAT, discovery server, and relay. For
   example, use `127.0.0.3` for the second NAT, `127.0.0.10` for discovery, and
   `127.0.0.20` for the relay. Reusing an IP defeats the restricted-cone test.
6. Call `router.SetBlocked(net.ParseIP("127.0.0.3"), true)` to drop the direct
   peer's packets in both directions. Keep the relay on a separate IP. Pass
   `false` to restore the direct path. Packets queued before blocking can still
   be read; drain completed exchanges before changing the path.
7. Use read deadlines to bound failed probes. Close the NAT when finished;
   `Close` closes all private and public sockets and joins receive goroutines.

Use `FullCone`, `RestrictedCone`, or `Symmetric` to select the behavior described
in [the model table](PLAN.md). Mappings remain stable for the life of the private
socket. Each symmetric destination has its own mapping, including different
ports at the same IP. The fixture does not predict external ports, expire
mappings, model IPv6, model hairpinning, or simulate packet reordering or latency.

The self-tests cover source reflection, cone-to-cone probes, symmetric-to-symmetric
failure using discovery candidates, peer blocking, relay forwarding, recovery,
deadlines, concurrent sends, and cleanup. Their discovery server reflects an
address string; it does not implement the STUN wire protocol. The relay is an
opaque local forwarder, not a paid production relay. To test production STUN or
WireGuard, adapt this packet connection to the transport's socket/bind seam.
Automatic fallback selection belongs to the production traversal tests.

Treat this fixture as evidence about simulated UDP behavior. Verify production
STUN parsing, WireGuard handshakes, paid rendezvous, automatic fallback, and
real networks separately before making demo claims.
