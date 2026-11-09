# Core-Sol ETH-279 path record

Demo beat 4: sponsored relay bootstrap and failover.

Paths written:

- `brambled/main.go`
- `brambled/wgnode/node.go`
- `brambled/live_demo.go`
- `brambled/datarelay_endpoint_test.go`
- `brambled/sponsored_relay_e2e_test.go`
- `brambled/netlab_role_test.go`
- `docs/ai-usage/core-sol-eth-279.md`

Verified: the initial bootstrap regression fails before the fix. Delaying cloud
admission for 12 seconds causes a second sponsored session before a handshake.
Disabling allocation retention also makes the delayed-renewal regression allocate
three sessions instead of two after its first publication times out.

The correction retains each unproven allocation until received traffic confirms
it or its server-provided expiry passes. Failed initial exchanges cache the
allocation by peer label and public key. Renewals record the previous received-byte
counter so old traffic cannot confirm a new session. Publications without traffic
back off even when rendezvous acknowledges them. The adopter limiter, server
sponsorship limits, byte caps, and privacy behavior stay unchanged.

Verified: the existing primary-relay kill tests reconnect through the backup,
including recovery after initial relay discovery fails. The existing sponsored
byte-cap test renews and returns HTTP 200.

The first local review reports zero Standards findings and three Spec findings.
The correction refreshes the byte baseline at endpoint switch, rechecks traffic
and admission after network calls, and applies endpoint changes with WireGuard's
atomic `update_only` setting. Publication does not edit the peer table.

Verified: removing `update_only` makes the admission regression restore a revoked
peer. Restoring it makes the test pass. The test also checks that transport
updates preserve allowed IPs. The depleted-budget integration test exhausts the
shared six-poll burst, verifies a 200-second default delay, then scales the limiter
interval to 15 seconds at the test boundary to exercise a real handshake. This
is not a measurement of the live 200-second recovery latency.

Open: ETH-244 owns installed-device and cloud acceptance. This task does not
modify or validate that runtime.
