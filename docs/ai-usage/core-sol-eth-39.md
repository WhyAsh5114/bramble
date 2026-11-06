# Core-Sol: W8d (demo beat 4)

## Paths written

- `brambled/rendezvous/endpoints.go`
- `brambled/stun/bind.go`
- `brambled/stun/punch.go`
- `brambled/traversal/controller.go`
- `brambled/traversal/controller_test.go`
- `brambled/wgnode/node.go`
- `brambled/wgnode/path.go`
- `docs/ai-usage/core-sol-eth-39.md`

## Verification

Verified on 6 November 2026: `go test -tags nattest ./traversal -count=1 -v`.
The rootless tests use real WireGuard, a loopback STUN responder, and simulated
restricted-cone and symmetric NATs. They cover direct pings, automatic relay
fallback, recovery after blocking the direct path, and rejected path updates for
a removed peer. Cloud NAT behavior remains Open.
