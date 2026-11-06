# ADR 0013: Tor transport for the sidecar's outbound traffic

**Status:** Draft, 6 November 2026. Human approver: pending.
**Workstream:** W9 (control plane over Tor), demo beat 6. See `docs/15_MUMBAI_HANDOFF.md` section 5.1.
**Scope of this ADR:** the Tor client module `sidecar/src/tor/`, its readiness contract, and how `brambled` waits for it. Routing the ENS reads and x402 payments through it is W9-3 (ETH-60) and builds on the interface fixed here.

## Context

Each node's sidecar polls ENS every 5 s through a Sepolia RPC endpoint and pays relays over x402. The RPC provider and the payment facilitator see the node's IP address, which registry it reads, and how often. The handoff requires all of the sidecar's outbound HTTP to go through Tor, with one isolation key per purpose, so the provider cannot link a node's reads to its payments.

Facts this decision rests on:

- tor-js 0.4.4 runs Arti (the Tor Project's Rust client) as WebAssembly and exposes `TorClient.fetch(url, init)` with `anonOptions.isolationKey`. Requests with different keys never share a circuit (Verified, 5 November spike).
- In the 5 November spike under Bun 1.3.14, the client was ready after 51 s with no snapshot. The first request took 21 s, later ones 4 to 5 s. About 1 in 10 first requests failed and succeeded when retried under a new isolation key (Verified, 5 November).
- On 6 November, under Bun 1.4.0 on the development machine, the client was ready after 8.4 s and 4.7 s in two runs, and a request took 3 to 6 s (Verified, `sidecar/scripts/tor-smoke.ts`). Bootstrap time varies by network and relay; plan for the 51 s figure.
- tor-js's default entry point downloads its WASM binary from jsDelivr, unpkg or GitHub in the clear and caches it under `~/.local/share`. Its default Node storage is also a directory under `~/.local/share` (Read from source, `node_modules/tor-js/dist/entryPoints/wasm-cdn/index.js`).
- `brambled serve` reads `/devices` and each peer's `mesh-ip` once at startup and exits if they fail. Once running, `admission.Loop` keeps a peer it has already admitted when a resolve fails (`handleResolveError`) (Read from source).

## Decision

1. **One Tor client per sidecar.** `sidecar/src/tor/` holds a single module-level client. `startTor()` creates and bootstraps it; repeated or concurrent calls share the one client.
2. **No CDN, no disk.** The client loads through `tor-js/wasm-file`, which reads the WASM binary from `node_modules`, and uses `storage.MemoryStorage`. Nothing is fetched outside Tor at startup and nothing is written to disk, so `pnpm check:no-local-state` stays green. The cost is a full bootstrap on every restart.
3. **One isolation key per purpose.** `torFetch(key)` takes `'ens'` or `'pay'` and sends every request under that purpose's current isolation key, `<key>:<random UUID>`. Different purposes never share a circuit.
4. **Retry once under a fresh key.** When a request fails with a tor-js `CONNECTION` error (or one marked `retryable`), `torFetch` replaces that purpose's isolation key with a fresh one and retries once. The fresh key stays in use, because Tor keeps using a failed circuit and the old key could fail for minutes. It does not retry an abort, a `RemoteConnectionRefused` (the destination's own answer), or any HTTP response, including 5xx. Concurrent failures on the same key rotate it once.
5. **Fail fast before readiness, never fall back to a direct connection.** Until the client is ready, `torFetch` rejects with `TorNotReadyError` ("starting Tor") instead of waiting or connecting directly.
6. **Readiness gate on the sidecar's HTTP API.** `GET /tor/status` reports `{ state: 'starting' | 'ready' | 'failed', readyAt?, lastError?, message }` and always answers 200. Every other route except `/health` answers 503 with `{ error, tor }` and `Retry-After: 5` until Tor is ready, where `error` is `'starting Tor'` while starting and `'Tor failed to start: <lastError>'` after a failed attempt, and `tor` is the status object. New routes are gated by default. `/health` stays process and tailnet identity only, so `brambled` still detects a stray sidecar within its 5 s window.
7. **Bootstrap deadline and retry.** Each bootstrap attempt has 180 s. A failed attempt sets `state: 'failed'` with `lastError` and starts another after 30 s. What happens to the client depends on how the attempt failed:
   - **Bootstrap error: new client.** tor-js 0.4.4 starts bootstrap in the `TorClient` constructor and caches that promise, so a client whose bootstrap failed rejects every later `ready()` and cannot be retried in place (Read from source, `node_modules/tor-js/dist`). The attempt closes that client and settles only once its relay sockets are closed; the next attempt creates a new one. Two clients never hold sockets at once.
   - **Timeout: same client.** The next attempt waits on the same bootstrap. A bootstrap still pending at 180 s is not stuck: Arti keeps retrying relays with backoff. tor-js also cannot cancel it. `TorClient.close()` leaves its direct TCP sockets open (ETH-107 review, Verified), and even with those closed, the closed client's bootstrap kept trying 2 to 3 relay dials per second in the shared WASM for the two minutes measured (Verified 6 November 2026). Replacing it would add one such loop per timeout. The cost: if that bootstrap finishes between attempts, `ready` is reported at the next attempt, up to 30 s later.
   - **Sockets.** tor-js's own `close()` closes only KPS gateway tunnels, so the transport gives tor-js a socket provider that dials every relay itself. On close it destroys every socket, including those still connecting, and refuses new dials. `stopTor()` resolves once they are closed.
8. **`brambled` waits for Tor at startup.** `sidecar.Start` polls `/tor/status` after `/health` until the state is `ready`, for up to 7 minutes (`Config.TorReadyTimeout`, default `DefaultTorReadyTimeout`). This covers a first attempt that runs its full 180 s, the 30 s wait, and a complete second attempt: 390 s. A 404 counts as ready, so a sidecar without the Tor module still starts. After startup, a 503 is an ordinary resolve error, and `admission.Loop` keeps the last-known peer set.

The interface other code depends on:

```ts
export type TorKey = 'ens' | 'pay'
export function torFetch(key: TorKey): typeof fetch
export function getTorStatus(): { state: 'starting' | 'ready' | 'failed'; readyAt?: number; lastError?: string }
export function startTor(opts?: StartTorOptions): Promise<void>
```

## Alternatives considered

- **Run a system `tor` daemon and use its SOCKS port.** Mature, with persistent state for fast restarts. Rejected: it adds a process and a disk-backed state directory to every node, and Bun's `fetch` has no SOCKS support, so it would need another proxy layer.
- **Wait inside `torFetch` until ready.** Simpler for callers, but a resolve would hang for up to a minute, and every brambled poll during bootstrap would pile up a pending request. A fast 503 lets `brambled` keep its last-known peers and try again on its next poll.
- **A fresh isolation key per request.** Strongest unlinkability between requests, but every request would build a new circuit (a 20 s first request in the spike), which breaks a 5 s poll. One key per purpose is what the handoff asks for: it separates reads from payments.
- **Gate only the ENS routes.** Payment routes would then fail later inside `torFetch('pay')` once W9-3 lands. Gating every data route gives one consistent "starting Tor" answer and gates future routes by default.
- **Persist the consensus to disk for fast restarts.** Breaks invariant 3 (no local-state writes in the node runtime path). The snapshot bootstrap (ETH-61, cut-first) is the sanctioned way to speed up cold start.

## Consequences

- A sidecar restart costs a full Tor bootstrap (51 s in the spike, 5 to 8 s on 6 November). `brambled` startup waits for it. The W11 preflight should check `/tor/status` on every node.
- The 503 gate ships before W9-3, so between the two merges the sidecar waits for Tor and then still reads ENS directly. W9-3 closes that gap.
- Exit relays see the destination host and, for plain HTTP, the content. Every RPC and facilitator URL must be HTTPS.
- The isolation key changes after a circuit failure, so the RPC provider sees a new exit IP. That is intended.
- tor-js is marked experimental by its authors. This ADR does not claim more than "requests leave through Tor exits".

## Verification

- Unit tests with a fake transport (`sidecar/test/tor.test.ts`, `sidecar/test/tor-routes.test.ts`, 24 tests, 6 November 2026): readiness states, one client under concurrent starts, bootstrap timeout keeping its client, a failed client replaced only after it has closed, request conversion for `fetch(url, init)` and `fetch(Request)`, per-key isolation, retry-once under a fresh key, no retry on abort, refusal or HTTP status, single rotation under concurrent failure, and the 503 gate (Verified).
- Real tor-js and WASM against a local server that accepts relay connections and never answers (`sidecar/test/tor-transport.test.ts`, 2 tests, Verified): `close()` destroys every relay socket of a pending bootstrap before it returns and no later dial goes out; a timed-out client is kept and no second client dials; `stopTor()` closes the pending bootstrap's sockets. Each part of the fix was removed in turn (socket destroy, dial refusal, keeping the client on timeout, waiting for close, `stopTor` waiting), and each removal failed at least one test (Verified).
- `brambled/sidecar/manager_test.go`: `waitTorReady` waits through `starting` and `failed`, times out with the sidecar's message, and treats 404 as ready (Verified).
- Real network, Bun 1.4.0, 6 November 2026: `cd sidecar && bun run scripts/tor-smoke.ts` printed "ready after 8.4 s", then `ens: HTTP 200 in 3.2 s, IsTor=true` and `pay: HTTP 200 in 5.5 s, IsTor=true` through two different exits. No `~/.local/share` directory existed after the run (Verified).
- Real sidecar process: `/health` answered 200 at once, `/tor/status` reported `starting Tor`, `/devices` answered 503, and after "Tor ready after 4.7 s" `/devices` passed the gate (Verified).

## Open

- Revocation latency with the ENS poll over Tor (ETH-61).
- Whether `@x402/hedera` makes network calls outside the wrapped fetch (ETH-58).
- Snapshot bootstrap for faster cold start (ETH-61, cut-first).
