# ADR 0002 — Go node agent, all ENS interaction through a local Hono/TypeScript sidecar

**Status:** Decided, Sept 5 2026 (firmed up from the original "sidecar permitted if needed" framing — the sidecar is now the required path, not a fallback). Chain-interaction logic verified working in TypeScript (Sept 5, Gate 0.3).

## Context

The node agent needs three things that are well-supported in Go and comparatively weak in TypeScript: userspace WireGuard (`wireguard-go`), STUN/ICE hole punching (`pion/ice`, `pion/stun`), and a long-running CLI daemon that's easy to distribute as a single binary. ENSv2, on the other hand, is a beta deployment (~3 weeks old at time of writing, still under audit) with custom, bespoke contracts and no Go SDK. `ensjs`/`viem` are the mature, ENSv2-aware, actively maintained path; hand-rolling Go ABI bindings against a moving, under-documented contract set is exactly the kind of thing that produces silent bugs rather than compile errors.

That risk isn't hypothetical: Gate 0.3's validation script already caught a real docs-vs-deployed-bytecode mismatch on `UserRegistryImpl.initialize` (see `12_SOURCE_NOTES.md`) — the published ENSv2 docs describe one initializer signature, the actually-deployed contract uses another, and the wrong one reverts silently with no helpful error. That was found once, in TypeScript, against a library that's tracking ENSv2's changes actively. Re-deriving the same contract surface by hand in Go means re-discovering drift like this independently, with a smaller community hitting the same edges first.

## Decision

The node agent is written in Go and does **no direct contract calls**. All ENS interaction (resolving the registry, reading EAC/role state, writing text records, watching for role changes) goes through a small local sidecar process, written in TypeScript on **Hono**, that the Go agent talks to over local HTTP (localhost, fixed port or a Unix domain socket).

- The sidecar wraps `ensjs`/`viem` and exposes a narrow, purpose-built API — e.g. `GET /registry/:name/state`, `GET /device/:subname` (pubkey, ACL, expiry, current role set), `POST /admin/enroll`, `POST /admin/revoke` — not a generic RPC proxy.
- **The sidecar is a per-node local process, not a shared service.** Each device runs its own Go agent and its own sidecar as a child process on that same host. This is load-bearing: `03_ARCHITECTURE.md`'s hard rule is that admission checking happens *at the peer*, using *that peer's own* resolution of the registry. A shared/central sidecar would quietly reintroduce the coordinator this project exists to remove. The sidecar changes *how* a peer resolves ENS, not *who* resolves it.
- Hono specifically because it's minimal and has near-zero framework overhead for what is a small, single-consumer internal API — no need for anything heavier.

## Rationale

Gate 0.3 validated the entire ENSv2 interaction surface — commit-reveal registration, subregistry deployment, EAC role delegation and restriction — in TypeScript/viem, end to end, against the live hackathon deployment, and it's the only path that's actually been proven. Choosing it as the sole path (rather than "Go first, sidecar as a fallback") avoids spending build time on two implementations of the same logic, and avoids the worse outcome of a Go implementation that looks like it works but silently mishandles something ENSv2-specific (mutable token IDs, the initializer drift above, or the next thing like it) because Go tooling for this contract set is unproven and effectively unmaintained by anyone but this project.

An HTTP sidecar over a stdin/stdout subprocess protocol was chosen because it's independently testable (curl-able, unit-testable with Vitest without the Go binary running at all), keeps the two processes decoupled (the sidecar can crash and restart without taking the Go agent's WireGuard/ICE state down with it), and needs no custom framing protocol.

## Consequence

- `05_BUILD_PLAN.md` Phase 1's discretion list no longer includes "whether Go calls contracts directly or via a TS sidecar" — it's decided.
- `04_TECH_STACK.md` gains Hono as a pinned dependency for the sidecar.
- Gate 1.1's admission-check path and Gate 2.1's EAC tests both cross the Go↔sidecar HTTP boundary. Neither gate's pass/fail criteria change; the sidecar must be running for either to pass, so test setup needs to start it alongside the node agent.
- The Go agent's resolver+cache component (`03_ARCHITECTURE.md`) is, concretely, "poll the local sidecar on a TTL and cache the response" — the architecture diagram's abstraction level doesn't need to change, this is an implementation detail one level down.
