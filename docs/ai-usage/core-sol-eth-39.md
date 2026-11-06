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

- `brambled/statusapi/path.go`
- `brambled/statusapi/path_test.go`
- `brambled/statusapi/server.go`
- `brambled/wgnode/stun.go`

- `brambled/main.go`
- `brambled/traversal.go`
- `brambled/traversal_test.go`
- `README.md`
- `brambled/README.md`
- `docs/reference/endpoint-exchange.md`

Verified: `go test . -run TestAdmissionTraversal -count=1 -v` uses metered
rendezvous and a fresh relay port for each allocation. Two admitted nodes share
one allocation, exchange encrypted traffic, and stop traversal on revocation.
`go test -race -tags nattest ./traversal ./statusapi -count=2` passes.

Review fixes: preserve the relay receive clock across failed direct retries;
identify forced-relay announcements without misreporting them as direct.
Both regression tests failed before their fixes and passed afterwards.

- `brambled/netlab_role_test.go`

The NAT lab role now accepts typed endpoint announcements through the production
parser. The live namespace/Docker lab is Open; this run does not execute it.

- `brambled/datarelay_endpoint_test.go`
- `brambled/sponsored_relay_e2e_test.go`
- `brambled/live_demo.go`

Existing relay test fixtures and the live-demo adopter use the production
announcement parser for typed relay candidates. The live demo remains Open.
