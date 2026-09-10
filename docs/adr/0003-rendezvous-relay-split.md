# ADR 0003 — Separate rendezvous relays (signaling) from the data-plane relay market, and meter both

**Status:** Decided, Sept 5 2026. Rendezvous relay + candidate exchange implemented and verified end-to-end Sept 6 2026 (`relay/`, `brambled/rendezvous/`, `10_DAY0_GATES.md` Gate 0.2). x402 metering of rendezvous usage (Phase 4) and the data-plane relay market are still unimplemented.

## Background, for anyone reading this without the rest of the NAT-traversal context

WireGuard peers talk directly to each other over UDP. To send the first packet, each side needs the other's reachable address — an IP:port. Most real devices sit behind NAT (home WiFi, phone hotspots, most VPSes behind a firewall), so their real address (`192.168.1.5`) means nothing from outside the network. Two ordinary devices can't just dial each other; neither has an address the other can use yet.

The standard fix is STUN + hole punching: each side asks a public STUN server "what do I look like from the outside?" (a *candidate*), then both sides fire UDP packets at each other's candidate at roughly the same time, which persuades most home routers to let the reply through. The part that's easy to miss: **step one — learning each other's candidate — needs a channel both sides can already reach, before any direct connection exists.** Tailscale's coordination server is that channel. This project has no such server by design, so something else has to carry that exchange. `03_ARCHITECTURE.md` assigns that job to relays.

**The café analogy:** two people with no phones want to meet in a big city. If each of them independently walks into whichever café looks cheapest that day, they'll never bump into each other — they need to have agreed on one specific café ahead of time. Relay selection in the original design ("price/latency, per connection") is exactly the "walk into whichever café looks cheapest" rule. It works fine once you already know where the other person is. It cannot be how you *find* them in the first place.

## Context

`03_ARCHITECTURE.md` designs the relay as a permissionless, price/latency-chosen market: any operator runs one, clients pick per connection. That design has a gap it doesn't itself address: before any connection exists, peer A has no way to know *which* relay instance to send peer B's candidate-exchange offer through, if relay choice is arbitrary and per-connection — each side would pick independently, with no shared meeting point. A single hand-run relay hides this during Gate 0.2's Day-0 test (there's only one relay to pick), but it surfaces at Gate 4.3 ("two relays, client chooses") — see `11_SOURCE_NOTES.md`, "Day 1 technical sanity check," item 5.

A second, separate problem surfaced once the first was solved: if data relays are only used when hole punching *fails*, and a lot of realistic topologies (e.g. a laptop connecting to a VPS, which has a public IP and needs no traversal at all on its side) succeed at hole punching essentially every time, then the paid data-relay path might rarely or never fire — which is a weak position for a track whose qualification bar is a real, metered, repeatedly-settled payment.

## Decision

Split the relay's two jobs, which the original design conflated, and meter both of them:

- **Rendezvous relays** — a small, ENS-published set per tailnet. Every member keeps light presence on these purely to exchange signaling blobs (STUN candidates, connection offers). Not price/latency-selected; this is infrastructure the tailnet owner points at, analogous to how the tailnet registry itself is discovered. **x402-metered, even though the payload is tiny.** Every connection attempt — hole-punch success or not — pays a small amount here. This is the fix for the second problem above: it guarantees real, repeated, on-chain payment activity regardless of how often the data relay is actually needed.
- **Data-plane relay** — unchanged from the existing design in mechanism: the permissionless, price/latency-chosen, x402-metered-per-byte market that forwards opaque UDP traffic when hole punching fails. Still needs to be exercised at least once in the demo (Gate 4.3 requires it structurally), but its payment volume is no longer the *only* evidence of a working paid relay service.

## Worked example

Alice's node agent wants to reach Bob's. It resolves ENS, confirms Bob is authorized, gets his public key — never his address, since addresses are never written on-chain. It STUNs its own candidate, then sends "I'm Alice, here's my candidate, I want Bob" to the tailnet's published rendezvous relay, paying the small metered fee for that message. Bob's agent, already listening on the same rendezvous relay, receives the offer and replies with its own candidate the same way. Both sides now have what they need to hole-punch directly. If that succeeds, the data relay is never touched — Alice and Bob talk directly from here on. If it fails, they fall back to a data relay chosen by price and latency, exactly as `03_ARCHITECTURE.md` already describes, paying per byte for as long as the connection needs the fallback.

## Trust boundary: what a rogue rendezvous relay can and can't do

This needs to be stated explicitly, because it's the difference between "another relay" and "a renamed coordinator" — and a judge will ask.

**Can: deny or lie about an address.** A malicious or down rendezvous relay can refuse to forward Alice's candidate to Bob (nothing connects), or forward a wrong one (say, an attacker's address instead of Bob's).

**Can't: make that lie succeed.** Alice already has Bob's real public key from ENS, independently of the relay. Her WireGuard handshake is addressed to that specific key, and the Noise_IK handshake only completes if whoever answers can prove they hold the matching *private* key. An attacker at a substituted address doesn't have it, so the handshake just times out — a doomed connection attempt, not a compromised one. The relay picked *where* Alice knocked; it never gets to decide *who's allowed to answer*. Symmetric in the other direction too (a fake "Alice" can't complete a handshake with Bob without Alice's private key). **Backed by a test, not just this argument** — `brambled/rendezvous/gate1_2_test.go`'s `TestGate1_2_MaliciousRelayCannotAdmitPeer` (Gate 1.2, `05_BUILD_PLAN.md`) runs a relay that fabricates a response claiming an uninvolved peer answered, against a real, live, responsive attacker node, and confirms no handshake is ever recorded.

**The residual risk that is real: selective censorship.** A rogue relay operator could quietly drop messages for one specific targeted pair while working normally for everyone else — indistinguishable from ordinary packet loss, much harder to notice than an outright outage. If a tailnet publishes only *one* rendezvous relay, that's a genuine single point of failure for connectivity (not admission — a narrower, but still real, weakness).

**Mitigation, now a requirement, not just a recommendation:** a tailnet's published rendezvous set must have more than one relay, and node agents must attempt candidate exchange across the *whole* published set (parallel query, or sequential retry on timeout — implementation detail, not a hard gate) rather than only the first one reached. One rogue or unreachable relay in the set must not be able to unilaterally block a connection that another relay in the same set would have carried. See `05_BUILD_PLAN.md` Gate 1.2's caution and `09_JUDGING.md` objection 14 for the judge-facing version of this argument.

**The clean contrast for a judge:** a compromised Tailscale/Headscale coordinator can inject a fully-legitimate-looking device — an admission-integrity failure, the whole trust model breaks. A rogue rendezvous relay can, at worst, deny connectivity for pairs routed through it — an availability failure, recoverable by trying another relay in the set. Different class of failure; the admission thesis in `03_ARCHITECTURE.md` is untouched either way.

## Rationale

This preserves the "no fixed DERP-style set" claim in `06_ADJACENT_WORK.md` for the part that matters (bulk data relay stays an open market) while giving discovery a concrete mechanism instead of leaving it implicit. Metering rendezvous costs almost nothing to add (it's the same x402 flow already needed for data relays, applied to a smaller payload) and directly hedges against the demo's own topology being *too well-behaved* to naturally need a data relay — a risk that cuts the opposite direction from `07_RISKS.md` K4 (which worries about *too much* relaying), and one the original design didn't account for either way.

## Consequence

- **✅ Resolved Sept 10, 2026** — the record-schema question below was open when this ADR was written; it's now built. Real discovery for both rendezvous and data relays, replacing the `-rendezvous`/`-data-relay` CLI flags Phase 4 shipped with initially: a relay registry, structurally separate from the tailnet's device registry (Gate 1.4's actual requirement — see `admincli/src/setup-relay-registry.ts`), discovered by scanning `LabelRegistered` events (not a manifest text record — `PermissionedRegistry` turned out to emit the plaintext label on every registration, verified directly against the real deployed registry, not assumed) and resolving each relay label's own `rendezvous-address`/`sidecar-url`/`price-per-byte` text records. Curated, not open self-registration: subname registration costs only gas, so `ROLE_REGISTRAR` on the relay registry stays EAC-gated to vetted operators, same trust model as device enrollment. The flags stay as an explicit override for local dev/testing. Full design, not re-litigated here: sidecar/src/ens/relays.ts, brambled/main.go's relay-discovery block.
- Gate 4.2 ("metered, not flat") now has two things to demonstrate cost scaling on: the (small, roughly flat) rendezvous fee per connection attempt, and the (bytes-scaled) data relay fee when the fallback path is used.
- The demo should deliberately include at least one topology that can't hole-punch (e.g. two devices both behind NAT, not laptop-to-VPS) so the data-relay path is shown live on camera, not just the rendezvous path.
- A tailnet's published rendezvous set needs at least two relays, and the node agent's candidate-exchange logic needs failover across the set (see "Trust boundary" above) — **✅ built**: `brambled/rendezvous.ExchangeAny` tries every discovered rendezvous relay in order, splitting the timeout budget across them rather than giving each the full window.
