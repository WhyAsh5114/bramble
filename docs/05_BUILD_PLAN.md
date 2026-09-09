# Build Plan

Gates are non-negotiable; discretion zones are the agent and user's call, recorded in `/docs/adr/`.

Complete `11_DAY0_GATES.md` first.

## Calendar (corrected Sept 7, verified against ethglobal.com/events/ethonline2026)

The event page lists the *finale* on Sept 16, but **project submissions close Sept 13, 16:00 UTC** — the original "Days 10–12" Phase 6 window (Sept 13–15) was scheduled entirely after the deadline and no longer exists. Real build time from Sept 7: **6.5 days.**

| Date (UTC) | Date (IST) | Milestone |
|---|---|---|
| Sept 4–6 | Sept 4–6 | Phase 1 complete (Days 1–3); Gates 0.2/0.3, 1.1–1.4 done |
| Sept 7 | Sept 7 | Watch the Ledger "Tracks Explained" recording (aired Sept 7 14:00 UTC / 19:30 IST — may resolve the `wallet-cli send` calldata question); resolve Gate 0.1; **run Gate 0.4 (Blocky402)** |
| **Sept 8, 03:59** | **Sept 8, 09:29** | **ETHGlobal Project Check-in #1 due** (showcase checkpoint — not optional) |
| Sept 8, 18:00 | Sept 8, 23:30 | Project Feedback Session #1 (cheap judge-proxy scrutiny — use it on the Phase 2 ACL story) |
| Sept 10, 13:00 | Sept 10, 18:30 | Project Feedback Session #2 |
| **Sept 11, 03:59** | **Sept 11, 09:29** | **ETHGlobal Project Check-in #2 due** |
| Sept 12 | Sept 12 | Build freeze. Record the video (human voice, 2–4 min; Hedera ≤5) |
| **Sept 13, 16:00** | **Sept 13, 21:30** | **Project submissions due** (video attached — `requireVideoSubmission` is on). Judging Round 1 (async) starts 19:00 UTC / Sept 14, 00:30 IST |
| Sept 14, 16:00 | Sept 14, 21:30 | Judging Round 2: live project judging |
| Sept 16, 16:00 | Sept 16, 21:30 | ETHOnline 2026 Finale (nothing is due after Sept 13) |

## Phase 1 — Node agent core (Days 1–3 — Sept 4–6; ✅ complete)

Userspace WireGuard, STUN, hole punching, relay fallback, and the admission verifier.

`HARD GATE 1.1 — The peer refuses independently.` A node must reject a handshake from a key that is not in the ENS registry, **using only its own resolution of that registry**. No central service participates in the decision.
**Test:** `test_PeerRejectsUnregisteredKey`. Stand up a node, present a valid, well-formed WireGuard handshake from an unregistered key, assert refusal. Then register the key, assert acceptance after cache expiry. This test is the project; film it.

`HARD GATE 1.2 — A lying coordinator changes nothing.` Run a deliberately hostile signaling relay that advertises an unauthorized peer as legitimate.
**Test:** `test_MaliciousRelayCannotAdmitPeer`. Assert the connection is refused. If this fails, the thesis is dead, not the test.
**✅ Verified Sept 6, 2026** — `brambled/rendezvous/gate1_2_test.go`'s `TestGate1_2_MaliciousRelayCannotAdmitPeer`. A hostile relay fabricates a response claiming Bob is at an attacker's address, without Bob ever being registered or involved at all. The attacker is a real, live, responsive `wgnode.Node` — not a stub. Alice still addresses her handshake to Bob's real public key (from ENS, independent of the relay); the attacker doesn't hold Bob's private key, so Noise_IK never completes and no handshake is ever recorded. This backs `docs/adr/0003-rendezvous-relay-split.md`'s "Trust boundary" claim with code, not just prose.
**Caution, scope:** this gate covers admission *integrity* — a hostile relay cannot get an unauthorized peer accepted, because a substituted or wrong address just produces a WireGuard handshake that fails (the attacker doesn't hold the real peer's private key). It does not cover *availability*: a rogue or down rendezvous relay can still deny connectivity for pairs routed through it (see `adr/0003-rendezvous-relay-split.md`, "Trust boundary"). Mitigate structurally: a tailnet's published rendezvous set must have more than one relay, and node agents must attempt candidate exchange across the whole set (parallel or sequential retry) rather than only the first one reached — one rogue or unreachable relay must not be able to unilaterally block a connection another relay in the set would have carried.

`HARD GATE 1.3 — Revocation propagates and is bounded.` Removing a device from the registry causes peers to drop it within a stated, measured window.
**Test:** measure the actual time from revocation transaction confirmation to connection drop. **Publish the number in the README.** It will be slower than Tailscale's push-based revocation. Say so; do not hide it (see `10_JUDGING.md`, objection 4).
**Caution:** ENSv2 token IDs are mutable — an EAC role grant or revoke burns and re-mints the token with a new ID (see `12_SOURCE_NOTES.md`). Detect revocation by re-reading the name's current role set, not by watching one token ID for a Transfer/burn event — an unrelated role change on the same name would emit an identical-looking event. **Not an issue here** — `sidecar/src/ens/client.ts`'s `resolveDevice` reads `getState(labelhash)` fresh by label every call, never by a cached token ID.
**Caution, design gap:** WireGuard has no teardown/revocation message. An already-established tunnel keeps passing traffic until either the local peer entry is actively removed from the wireguard-go config, or WireGuard's own rekey timer fires (~2 min by default). If the admission verifier only gates *new* handshakes, "time to connection drop" reads fast for a peer that hasn't connected yet, while an *already-connected* revoked peer stays up for up to ~2 minutes regardless — a different, more attack-relevant number. The registry-refresh loop must actively remove revoked peers from the live wireguard-go peer table on every poll, not just refuse future handshakes. State explicitly which scenario the measured number covers, and ideally measure both.
**✅ Mechanism verified Sept 6, 2026** — `brambled/admission/gate1_3_test.go`'s `TestGate1_3_RevocationDropsAlreadyConnectedPeer` proves the already-connected case drops immediately once `SyncOnce` removes the peer (bounded by `-ttl`, not the ~2 minute rekey timer), using a real `wgnode.Node` peer table, not a fake one. **Writing this test caught a real bug**: revoking by *clearing* a device's `pubkey` text record (the only proven ENS write primitive available — no verified `setExpiry`/`renew`-to-the-past exists) left the peer stuck in the live table forever, because the loop had no way to recover which pubkey to remove once the record stopped reporting one. Fixed in `admission/loop.go` by having `Loop` remember the last pubkey each label was authorized under. **✅ Live number measured Sept 6, 2026** — real VPS (device2) + laptop (device1) run, `-ttl 5s`, following `brambled/README.md`'s "Gate 1.3 runbook." Revoked device2 by clearing its `pubkey` text record via `set-pubkey.ts`; on-chain confirmation at `11:37:01.819Z`. Timestamped `ping -D` on the laptop showed the last successful reply at `11:37:03.54Z` (+1.7s) and permanent connection drop (zero recovery afterward) at `11:37:05.53Z` (**+3.7s from revocation confirmation to connection drop, already-connected peer**) — bounded by the 5s `-ttl` used, nowhere near WireGuard's ~2 minute rekey timer. Slower than Tailscale's push-based revocation, as expected; this is the cost of "no trusted coordinator to push a revoke," and it's still sub-4-second at a demo-realistic `-ttl`.

`HARD GATE 1.4 — No endpoints on device or agent subnames.` Grep the codebase: no IP address, port, or endpoint is ever written to a device or agent (mesh-member) ENS record. Relay subnames are the sole exception (see `03_ARCHITECTURE.md`) — any write path touching a relay's record must be structurally incapable of also touching a device/agent record (separate sub-registry, not just a runtime check).
**Test:** CI check that fails on any write path writing endpoint data into the device/agent sub-registry namespace. Also a manual read of every record the demo writes, confirming relay and device/agent records never share a namespace.
**✅ Verified Sept 6, 2026** — `scripts/check-no-endpoints.mjs`, wired into `pnpm verify`/CI. Scans every `setText()` call site under `sidecar/src`, `scripts/provision-dev-tailnet`, and `brambled` (excluding the frozen `scripts/gate0.3-eac-check` artifact and `relay/`, the one namespace allowed to carry endpoint data) for a banned record key (`endpoint`/`address`/`ip`/`host`/`port`/`url`) or an IP:port-shaped literal argument. Manual read confirms the only current writes (`register-device2.ts`, `index.ts`) ever write `pubkey`, never endpoint data. Known limit, stated plainly: a value passed through a variable rather than a literal can't be caught by grep — this check is a regression gate against a *known-shape* violation, not a substitute for the manual read Phase 4 will need again once a relay ENS write path exists.

**Discretion:** cache TTL, resolution strategy, ICE configuration, relay protocol framing. ENS interaction is decided, not discretionary: the node agent calls a local per-node Hono/TypeScript sidecar over HTTP rather than making contract calls directly — see `adr/0002-node-agent-language.md`.

## Phase 2 — ENSv2 registry and ACLs (Days 4–5 — Sept 7–8)

`HARD GATE 2.1 — EAC is load-bearing, not decorative.` Enrollment, revocation, and key rotation are governed by distinct EAC roles, and an account without a role genuinely cannot perform the action.
**Test:** table-driven test, one case per role × action, asserting permitted and denied cases on Sepolia. ENS judges explicitly require features to be central, not cosmetic.
**Caution:** every grant/revoke in this test changes the target token's ID (mutable token IDs, see `12_SOURCE_NOTES.md`). Re-query the current token ID after each step rather than asserting against one captured at test start.
**✅ Verified Sept 7, 2026** — `admincli/test/gate2.1-eac-check.ts`, run end-to-end against the live hackathon Sepolia deployment (real dev tailnet from `scripts/provision-dev-tailnet`, real transactions, real revert checks — not simulated setup). Three distinct EAC roles, one per action (design in `docs/adr/0004-admin-cli-writes-directly-to-registry.md`): `ROLE_REGISTRAR` (root-scoped on the subregistry, gates `register()`) for enroll, `ROLE_SET_TEXT` scoped to the `pubkey` setter for rotate, `ROLE_SET_TEXT` scoped to the `revoked` setter (a new record, additive to the existing clear-`pubkey` revocation path Gate 1.3 already verified) for revoke. Granted to three freshly generated, never-funded test accounts via three real on-chain transactions (`grantRootRoles`/`grantSetterRoles`), confirmed on Sepolia. All 12 cases in the {enroller, rotator, revoker, no-role} × {enroll, rotate, revoke} table matched expectation: each role permitted only its own action and was denied (`EACUnauthorizedAccountRoles`-class revert, via `simulateContract` — Gate 0.3's proven technique) every other action, and the no-role account was denied all three. `ROLE_REGISTRAR` was verified from `ensdomains/contracts-v2`'s primary source (`PermissionedRegistry.sol`, `RegistryRolesLib.sol`), not guessed or taken from the docs page alone — see ADR 0004 for why this mattered (it is a distinct grant target from the existing `REGISTRATION_ROLE_BITMAP`, which governs the new token owner's rights, not who may call `register()`).

`HARD GATE 2.2 — Nothing hard-coded in the demo path.` ENS qualification says the demo must be functional and not include hard-coded values.
**Test:** delete every local cache, restart both nodes, confirm the mesh reassembles purely from chain state.

`HARD GATE 2.3 — ACLs are enforced at the peer.` A device may reach exactly what its ACL permits.
**Test:** `test_AgentCannotReachOutsideACL`. Agent with a `db` grant sends `CONNECT cache` on the same host and `CONNECT db` on a different host entirely; both refused by the receiving peer. (Record schema: symbolic service names published as ECDH digests between the vouching identity's and target host's already-published pubkeys, not `host:port` — see `adr/0005-acl-record-schema.md`, Phase 2 Section B. Each gateway verifies only against granters listed in its own `acl-granters` record — no tailnet-wide admin key involved.)
**✅ Verified Sept 8, 2026** — `brambled/gateway/server_test.go`'s `TestGate2_3_AgentCannotReachOutsideACL`, run against three real `wgnode.Node`s with real WireGuard handshakes (netstack, per existing Gate 1.2/1.3 precedent — not a live two-machine run, which isn't required for this gate). All three required cases pass: `CONNECT db` on the authorized gateway succeeds and proxies real bytes end to end from a real local backend process; `CONNECT cache` on the same gateway is denied; `CONNECT db` on a second, separately-keyed gateway that trusts the *same* granter is denied too — proving the ECDH digest is bound to one specific gateway pubkey, not just "an authorized granter said so." Full design and the gateway CONNECT protocol itself: `adr/0006-gateway-connect-protocol.md`. 11/11 tests pass in `brambled/gateway`; full `brambled` suite green, no regressions.

**Discretion:** record schema, whether to use wildcard resolution or a deployed subname registry, ENSIP-25/26 record keys (recommended), expiry granularity.

## Phase 3 — Ledger enrollment (Days 6–7 — Sept 9–10)

Skip entirely if Gate 0.1 failed.

**Prerequisite, due Sept 7:** Gate 0.1 (physical device) *and* the `wallet-cli send` calldata question (`adr/0001`'s open verification) must both be resolved before this phase starts — Phase 3 gets 2 days and cannot absorb a mid-phase discovery. The Ledger "Tracks Explained" workshop aired Sept 7; check its recording first.

`HARD GATE 3.1 — Device confirmation gates permission changes.` Enrolling or revoking requires a physical button press. No software path bypasses it.
**Test:** attempt enrollment with the device disconnected; assert failure.

`HARD GATE 3.2 — The headless host never holds a plaintext key.` The VPS node's WireGuard private key is stored encrypted under `wallet-cli ring`.
**Test:** inspect the host filesystem; assert no plaintext private key. Document the LKRP rotation limitation (see `adr/0001-ledger-ring-vs-send-split.md`): after a ring member is removed, previously encrypted data cannot be decrypted, so key material must be re-encrypted on membership change.

`HARD GATE 3.3 — Describe "ring" accurately.` It is encryption, not signing. Signing goes through `wallet-cli send` with on-device confirmation.
**Test:** grep README and video script for any claim that `ring` signs anything.

**Discretion:** enrollment UX, whether the admin CLI wraps `wallet-cli` or instructs the user to run it.

## Phase 4 — Relay and x402 metering (Days 7–8 — Sept 10–11)

**Prerequisite:** Gate 0.4 (one real Blocky402 payment) must be run by Sept 8 — it is still unrun, and this entire phase sits on top of it.

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

## Phase 5 — Agent path (Days 8.5–9 — Sept 11–12)

`HARD GATE 5.1 — The agent is real but thin.` A genuine process needing private access, not an LLM demo. If agent framework code is being written on day 10, that is drift.
**Test:** the agent completes a real task through the mesh (query the private database, hit the internal API) and the transcript shows it.

`HARD GATE 5.2 — The agent holds no transferable credential.` No API key, no password, no long-lived token anywhere in the agent's environment.
**Test:** dump the agent's environment and config; assert the only secret is a node key that is worthless off-host and once revoked.

## Phase 6 — Demo, README, submission (Days 9–10 — Sept 12 to Sept 13 AM)

Video recorded by Sept 12; Sept 13 morning is upload/rehearsal buffer only. **Submissions close Sept 13, 16:00 UTC / 21:30 IST — hard.**

All gate tests green from a clean checkout. README covering setup, architecture, the on-chain boundary, measured revocation latency, and declared dependencies. `/docs/AI_USAGE.md` and `/docs/adr/`. Select ENS, Ledger, Hedera.

## Cut order

With 6.5 build days remaining as of Sept 7, treat this as a **decision to make by end of Sept 10** (end of Phase 3), not a day-11 contingency. If Phases 3 and 4 are both behind schedule on Sept 10, pre-commit to ENS + Hedera (the rendezvous fee alone satisfies Gate 4.1's "one real paid request" requirement) rather than discovering the cut on Sept 12 while the video is due.

1. HCS audit trail
2. Second relay (keep the selection mechanism, demo with one and say so)
3. Ledger Key Ring encryption → keep only `wallet-cli send` device confirmation
4. Ledger entirely → two slots
5. Agent path → mesh only, and drop Hedera's agent framing (weakens that slot badly)
6. x402 metering → ENS + Ledger only

**Never cut:** peer-side admission verification, the malicious-relay test, no-endpoints-on-chain, revocation, commit cadence.

## Future work (explicitly not scoped for this hackathon)

Ideas that are good next steps but not needed for any gate above, and would add integration risk if pulled in now. Recorded so they don't get silently forgotten or re-litigated in every conversation.

- **Auto-discover peers from the tailnet registry, instead of manual `-peer label=allowed-ip` flags.** Requires two things not yet verified: (1) the deployed `PermissionedRegistry` may not expose an enumeration view function (`ownerOf`/`getState` need an ID you already have) — enumerating registered subnames would likely mean scanning registration/Transfer-style event logs from the deployment block forward, untested against this deployment; (2) a mesh-IP assignment scheme, since `AllowedIP` today is operator-supplied and not derivable from ENS state (`admission/loop.go`'s `Peer` struct explicitly defers this to "a record schema question Phase 2 owns"). `admission.Loop.SyncOnce` already just iterates whatever's in `l.Peers`, so a discovery source slotting in later wouldn't require touching the sync/endpoint-resolution logic. Not a security downgrade to defer — the actual admission boundary is the WireGuard handshake against ENS-resolved keys either way; `-peer` only controls which labels a node bothers polling.
- **Automatic AllowedIP assignment**, e.g. a deterministic mesh-IP-from-ENS-record scheme, so operators stop hand-assigning `/32`s. Blocks on the same mesh-IP record schema as the point above.
- **Admin UI / web dashboard** — see all peers in a tailnet with live routing info (AllowedIPs, current endpoint, last handshake), node info (pubkey, ENS fullname, role/expiry), and rendezvous info (which relay(s) a peer is reachable through). Effectively a live view over what `brambled serve`'s stderr log already prints per-event, rendered instead of scrolled.
- **ENS registry explorer** scoped to a tailnet — browse registered devices/agents and their record state without needing `brambled resolve` per label, useful for debugging admission issues live during a demo or dev session.
- **"Test connection to peer" tooling** — a one-shot ping/handshake-status check against a specific peer from the UI or CLI, surfaced without needing to shell out to `ifconfig`/`ping`/`nc` manually (see `brambled/README.md`'s Gate 0.2 runbook, which currently requires exactly that).

## Reporting checkpoints

- Day 0 gates: 0.2/0.3 resolved Sept 5–6. **Gate 0.1 (Ledger device) and Gate 0.4 (Blocky402) are still open, due Sept 7–8** — with submissions closing Sept 13, an unverified Ledger rail or Blocky402 discovered late kills a sponsor slot, not just a phase.
- Gate 0.2 status: resolved Sept 6 (laptop-to-VPS; the harder two-NAT case remains open).
- End of Phase 1: ✅ malicious-relay test passing, plus measured revocation latency (3.7s @ 5s TTL, Sept 6).
- **Sept 8, 03:59 UTC / 09:29 IST: Project Check-in #1. Sept 11, 03:59 UTC / 09:29 IST: Check-in #2.** ETHGlobal showcase requirements — see `09_EVENT_RULES.md`.
- **End of Phase 3 (Sept 10): cut-order decision** (see Cut order).
- Immediately if EAC delegation does not actually restrict (Gate 2.1).
- Immediately if relay fallback rate is so high that direct connections rarely succeed — that changes the cost story in `01_WHY.md`.
