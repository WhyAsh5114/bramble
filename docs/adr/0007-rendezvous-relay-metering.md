# ADR 0007 — Meter the rendezvous relay with real x402/Hedera payments

**Status:** Decided, Sept 9 2026. Builds on `adr/0003-rendezvous-relay-split.md` (the rendezvous/data-relay split) and `adr/0002-node-agent-language.md` (Go node agent, TypeScript sidecar for anything without a mature Go SDK). Gate 0.4 (`11_DAY0_GATES.md`) already proved the payment rail standalone; this ADR wires it into the actual rendezvous relay.

## Context

`adr/0003` decided every connection attempt pays a small, roughly-flat rendezvous fee, hole-punch success or not. It didn't decide *how* — that's this ADR's job, and three questions turned out not to have obvious answers.

**Where does the x402/Hedera logic run?** Checked directly: no Go implementation of x402 supports Hedera. `x402-foundation/x402/go` — the only maintained Go SDK — covers EVM and Solana only. Building this natively in Go would mean reverse-engineering Blocky402's exact wire format from the TypeScript reference implementation (no spec document covers it) and hand-rolling Hedera transaction construction/signing, with no official implementation to check against. This is the same shape of risk `adr/0002` already named for ENS ("hand-rolling Go ABI bindings against a moving, under-documented contract set is exactly the kind of thing that produces silent bugs rather than compile errors") — and the fix is the same: put the chain-touching logic in TypeScript, behind a local Hono sidecar, reusing `@x402/core`/`@x402/hedera`/`@x402/fetch`/`@x402/hono`, all confirmed real and working in Gate 0.4.

**What does a fee actually meter?** `relay/main.go`'s wire protocol is newline-delimited JSON over TCP: a client sends `hello` once per connection, then an `offer` — and if the other side hasn't registered yet, the relay replies with an immediate error and the client **retries the offer every second** until it's acked (`relay/main.go`'s protocol comment, `brambled/rendezvous/client.go`'s `Exchange`). The two sides "won't generally call this at the exact same instant" — this is the common case, not an edge case. Metering raw `offer` messages 1:1 would nondeterministically multi-charge a single logical connection attempt based on how the retry race happened to land — the opposite of "roughly flat." The fix: meter the *session*, not the message. One token, bought once per `Exchange()` call, covers however many `offer` retries that one call needs.

**What asset?** Testnet USDC (`0.0.429274`, HTS), not native HBAR as Gate 0.4 used. Deliberate choice to reach the "HTS tokens or custom fee schedules in the settlement path" extra-points bullet (`02_TRACK_FIT.md`) now that this is a real feature, not a throwaway proof — the token-association step (`TokenAssociateTransaction`, `@x402/hedera`'s README) is a one-time setup cost per account, not a per-payment one.

One more thing worth recording since it shapes the whole design: `admission/loop.go`'s `EndpointResolver` — which is what calls `Exchange`, which is what pays — **fires at most once per peer for the life of the Loop**. Once an endpoint is resolved it's never re-resolved. There's no runaway-repeated-charging failure mode to design against; "one connection attempt, one payment" is already the natural cadence.

## Decision

A new local sidecar, `relay-sidecar/` (Hono on Bun, same shape as the existing ENS sidecar), runs alongside the Go relay and does the actual chain-touching work. It exposes one public, x402-gated route, `POST /rendezvous-token`, priced at 0.01 USDC (`{asset: "0.0.429274", amount: "10000"}`, an explicit `AssetAmount` — bypassing `Money`-string USD conversion, same as Gate 0.4), settled through Blocky402. On successful settlement it mints and returns a **self-contained, HMAC-signed token** — not a database row, not a second HTTP call the Go relay has to make on its hot path.

**Token wire format**, fixed here so `relay-sidecar` (mint) and `relay/` (verify) can't drift apart:

- Payload: compact JSON, fixed key order, `{"exp":<unix seconds>,"nonce":"<32 hex chars>"}` (nonce = 16 random bytes).
- `token = base64url(payload) + "." + hex(HMAC-SHA256(secret, base64url(payload)))`.
- TTL: 60 seconds from mint — generous against the 1-second retry cadence, tight enough that a genuinely new attempt can't reuse an old token.

The **secret is never operator-configured and never touches ENS or any chain** — it's the relay's own anti-freeloading measure, orthogonal to the mesh's admission model. `relay/main.go`, when started with `-meter`, generates 32 random bytes at startup, spawns `relay-sidecar` as a child process (mirroring `brambled/sidecar.Manager`'s exec+health-check pattern, not shared code — this one's smaller and has no tailnet identity to match), and hands the secret over via an env var. The Go relay verifies every `hello`'s token **entirely locally** — recompute the HMAC, check `exp`, check the nonce hasn't been seen before (in-memory, lazily pruned) — no network call back to the sidecar in the per-connection path.

On the paying side, the existing per-node ENS sidecar (`sidecar/`, `adr/0002`) gains a new route, `POST /rendezvous-token`, rather than spinning up a third per-node process. It pays the relay's public endpoint via `@x402/fetch`'s `wrapFetchWithPaymentFromConfig` + `@x402/hedera`'s `ExactHederaScheme`/`createClientHederaSigner` — the identical pattern `scripts/gate0.4-blocky402-check/client.mjs` already proved — and hands the resulting token back to `brambled` over the same local HTTP boundary every other sidecar call already uses. `brambled/rendezvous.Exchange` gains a `token` parameter, sent as `hello`'s new `Token` field; `main.go`'s `EndpointResolver` closure fetches a fresh token immediately before each `Exchange` call.

Metering is strictly opt-in (`-meter`, default off) — an unmetered relay needs no token at all (the field is `omitempty`), so Gate 0.2/1.2/1.3/2.2/2.3's already-verified tests need zero changes.

## Rationale

This keeps the trust boundary `adr/0003` already established untouched: the relay still forwards opaque blobs and has zero admission authority. The token secret and the HMAC check are purely about "did someone pay," never about "is this pubkey allowed to talk to that one" — that question is still answered exclusively by each peer's own ENS resolution, nowhere near this code. Putting the chain logic in TypeScript instead of Go isn't a compromise; it's the same call `adr/0002` already made, for the same reason (no mature Go SDK, real risk of silent protocol drift), now generalized past ENS specifically to "any chain interaction without a mature, actively-maintained Go implementation."

## Consequence

- `relay/main.go` gains a runtime dependency on a Bun/Node process being available at `-sidecar-dir` when `-meter` is set — same operational shape `brambled` already has with its own ENS sidecar.
- `05_BUILD_PLAN.md` Gate 4.1 is satisfied once the live run (documented in `relay-sidecar/README.md`, same evidence style as Gate 0.4's) produces a real settlement tx.
- Section C (data-plane relay metering, bytes-scaled) is a separate fee and a separate design question — this ADR's token scheme is specific to the rendezvous relay's low-frequency, session-shaped usage pattern, not a template to copy unmodified for per-byte billing.
