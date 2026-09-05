# Build Plan

Sept 4–16. Gates are non-negotiable; discretion zones are the agent and user's call, recorded in `/docs/adr/`.

Complete `11_DAY0_GATES.md` first.

## Phase 1 — Node agent core (Days 1–3)

Userspace WireGuard, STUN, hole punching, relay fallback, and the admission verifier.

`HARD GATE 1.1 — The peer refuses independently.` A node must reject a handshake from a key that is not in the ENS registry, **using only its own resolution of that registry**. No central service participates in the decision.
**Test:** `test_PeerRejectsUnregisteredKey`. Stand up a node, present a valid, well-formed WireGuard handshake from an unregistered key, assert refusal. Then register the key, assert acceptance after cache expiry. This test is the project; film it.

`HARD GATE 1.2 — A lying coordinator changes nothing.` Run a deliberately hostile signaling relay that advertises an unauthorized peer as legitimate.
**Test:** `test_MaliciousRelayCannotAdmitPeer`. Assert the connection is refused. If this fails, the thesis is dead, not the test.
**Caution, scope:** this gate covers admission *integrity* — a hostile relay cannot get an unauthorized peer accepted, because a substituted or wrong address just produces a WireGuard handshake that fails (the attacker doesn't hold the real peer's private key). It does not cover *availability*: a rogue or down rendezvous relay can still deny connectivity for pairs routed through it (see `adr/0003-rendezvous-relay-split.md`, "Trust boundary"). Mitigate structurally: a tailnet's published rendezvous set must have more than one relay, and node agents must attempt candidate exchange across the whole set (parallel or sequential retry) rather than only the first one reached — one rogue or unreachable relay must not be able to unilaterally block a connection another relay in the set would have carried.

`HARD GATE 1.3 — Revocation propagates and is bounded.` Removing a device from the registry causes peers to drop it within a stated, measured window.
**Test:** measure the actual time from revocation transaction confirmation to connection drop. **Publish the number in the README.** It will be slower than Tailscale's push-based revocation. Say so; do not hide it (see `10_JUDGING.md`, objection 4).
**Caution:** ENSv2 token IDs are mutable — an EAC role grant or revoke burns and re-mints the token with a new ID (see `12_SOURCE_NOTES.md`). Detect revocation by re-reading the name's current role set, not by watching one token ID for a Transfer/burn event — an unrelated role change on the same name would emit an identical-looking event.
**Caution, design gap:** WireGuard has no teardown/revocation message. An already-established tunnel keeps passing traffic until either the local peer entry is actively removed from the wireguard-go config, or WireGuard's own rekey timer fires (~2 min by default). If the admission verifier only gates *new* handshakes, "time to connection drop" reads fast for a peer that hasn't connected yet, while an *already-connected* revoked peer stays up for up to ~2 minutes regardless — a different, more attack-relevant number. The registry-refresh loop must actively remove revoked peers from the live wireguard-go peer table on every poll, not just refuse future handshakes. State explicitly which scenario the measured number covers, and ideally measure both.

`HARD GATE 1.4 — No endpoints on device or agent subnames.` Grep the codebase: no IP address, port, or endpoint is ever written to a device or agent (mesh-member) ENS record. Relay subnames are the sole exception (see `03_ARCHITECTURE.md`) — any write path touching a relay's record must be structurally incapable of also touching a device/agent record (separate sub-registry, not just a runtime check).
**Test:** CI check that fails on any write path writing endpoint data into the device/agent sub-registry namespace. Also a manual read of every record the demo writes, confirming relay and device/agent records never share a namespace.

**Discretion:** cache TTL, resolution strategy, ICE configuration, relay protocol framing. ENS interaction is decided, not discretionary: the node agent calls a local per-node Hono/TypeScript sidecar over HTTP rather than making contract calls directly — see `adr/0002-node-agent-language.md`.

## Phase 2 — ENSv2 registry and ACLs (Days 3–5)

`HARD GATE 2.1 — EAC is load-bearing, not decorative.` Enrollment, revocation, and key rotation are governed by distinct EAC roles, and an account without a role genuinely cannot perform the action.
**Test:** table-driven test, one case per role × action, asserting permitted and denied cases on Sepolia. ENS judges explicitly require features to be central, not cosmetic.
**Caution:** every grant/revoke in this test changes the target token's ID (mutable token IDs, see `12_SOURCE_NOTES.md`). Re-query the current token ID after each step rather than asserting against one captured at test start.

`HARD GATE 2.2 — Nothing hard-coded in the demo path.` ENS qualification says the demo must be functional and not include hard-coded values.
**Test:** delete every local cache, restart both nodes, confirm the mesh reassembles purely from chain state.

`HARD GATE 2.3 — ACLs are enforced at the peer.` A device may reach exactly what its ACL permits.
**Test:** `test_AgentCannotReachOutsideACL`. Agent with a `db:5432` grant attempts port 22 on the same host and a different host entirely; both refused by the receiving peer.

**Discretion:** record schema, whether to use wildcard resolution or a deployed subname registry, ENSIP-25/26 record keys (recommended), expiry granularity.

## Phase 3 — Ledger enrollment (Days 5–7)

Skip entirely if Gate 0.1 failed.

`HARD GATE 3.1 — Device confirmation gates permission changes.` Enrolling or revoking requires a physical button press. No software path bypasses it.
**Test:** attempt enrollment with the device disconnected; assert failure.

`HARD GATE 3.2 — The headless host never holds a plaintext key.` The VPS node's WireGuard private key is stored encrypted under `wallet-cli ring`.
**Test:** inspect the host filesystem; assert no plaintext private key. Document the LKRP rotation limitation (see `adr/0001-ledger-ring-vs-send-split.md`): after a ring member is removed, previously encrypted data cannot be decrypted, so key material must be re-encrypted on membership change.

`HARD GATE 3.3 — Describe "ring" accurately.` It is encryption, not signing. Signing goes through `wallet-cli send` with on-device confirmation.
**Test:** grep README and video script for any claim that `ring` signs anything.

**Discretion:** enrollment UX, whether the admin CLI wraps `wallet-cli` or instructs the user to run it.

## Phase 4 — Relay and x402 metering (Days 7–9)

`HARD GATE 4.1 — One real paid request settles on Hedera testnet via Blocky402, on camera.` Hedera qualification requirement. Not mocked, not local.
**Test:** E2E script prints a transaction ID viewable on HashScan.
**Note:** the rendezvous-relay fee (see Gate 4.2) already satisfies this on its own, every connection attempt — do not let this gate depend on the data-relay fallback firing naturally.

`HARD GATE 4.2 — Metered, not flat.` Two transfers of different sizes cost different amounts.
**Test:** assert cost scales with bytes. Hedera awards extra points for metering over flat charges.
**Design constraint:** this must gate a session/bandwidth-allotment purchase, not individual packets — a literal per-packet HTTP 402 challenge is incompatible with real-time UDP tunneling. Settle periodically against accumulated usage (see `adr/0003-rendezvous-relay-split.md` and `12_SOURCE_NOTES.md` finding 5).
**Two fees, not one, per `adr/0003-rendezvous-relay-split.md`:** a small, roughly-flat rendezvous fee charged on every connection attempt (candidate exchange, regardless of whether hole punching succeeds), and a bytes-scaled data-relay fee charged only when hole punching fails and traffic actually needs forwarding. The rendezvous fee exists specifically so real, repeated, on-chain payment activity doesn't depend on the demo's topology happening to need a data relay — a laptop-to-VPS connection, for instance, will likely hole-punch (or just connect directly, since the VPS has no NAT) every time.
**Test, additional:** assert the rendezvous fee is charged on a connection that *successfully* hole-punches and never touches the data relay.

`HARD GATE 4.3 — Two relays, client chooses.` Even if both are yours, the selection mechanism must be real: different prices, client picks, connection survives killing one.
**Test:** kill the selected relay mid-transfer; assert failover.
**Demo requirement:** at least one demoed connection must use a topology that cannot hole-punch (e.g. two devices both behind NAT — see `11_DAY0_GATES.md` Gate 0.2's note on laptop-to-VPS being the easy case) so the data-relay path is shown live, not just asserted in a test.

**Discretion:** pricing units, settlement batching, whether relays register in an ENS directory (recommended — Hedera extra points for agent discovery), HCS audit trail (extra points, optional).

## Phase 5 — Agent path (Days 9–10)

`HARD GATE 5.1 — The agent is real but thin.` A genuine process needing private access, not an LLM demo. If agent framework code is being written on day 10, that is drift.
**Test:** the agent completes a real task through the mesh (query the private database, hit the internal API) and the transcript shows it.

`HARD GATE 5.2 — The agent holds no transferable credential.` No API key, no password, no long-lived token anywhere in the agent's environment.
**Test:** dump the agent's environment and config; assert the only secret is a node key that is worthless off-host and once revoked.

## Phase 6 — Demo, README, submission (Days 10–12)

All gate tests green from a clean checkout. README covering setup, architecture, the on-chain boundary, measured revocation latency, and declared dependencies. `/docs/AI_USAGE.md` and `/docs/adr/`. Select ENS, Ledger, Hedera.

## Cut order

1. HCS audit trail
2. Second relay (keep the selection mechanism, demo with one and say so)
3. Ledger Key Ring encryption → keep only `wallet-cli send` device confirmation
4. Ledger entirely → two slots
5. Agent path → mesh only, and drop Hedera's agent framing (weakens that slot badly)
6. x402 metering → ENS + Ledger only

**Never cut:** peer-side admission verification, the malicious-relay test, no-endpoints-on-chain, revocation, commit cadence.

## Reporting checkpoints

- End of Day 0: all gates.
- **Sunday Sept 6 night: Gate 0.2 status. This is the go/no-go for the whole project.**
- End of Phase 1: the malicious-relay test passing, plus measured revocation latency.
- Immediately if EAC delegation does not actually restrict (Gate 2.1).
- Immediately if relay fallback rate is so high that direct connections rarely succeed — that changes the cost story in `01_WHY.md`.
