# bramble sidecar

Per-node local HTTP service wrapping `viem` for all ENS interaction, per
`docs/adr/0002-node-agent-language.md`. Not a shared service — one instance
per node, spawned as a child process by `brambled` (see `brambled/sidecar/`).

## Phase 1 Section A scope

Read-only. One endpoint: `GET /device/:label`, resolving a device subname
within this sidecar's own configured tailnet to `{ fullname, pubkey, status,
expiry, tokenId }`.

**Deviations from the ADR's illustrative sketch, made while implementing:**

- **`roleBitmap` is not returned.** Gate 0.3 proved EAC _write_ restriction
  (a delegated account can be blocked from writing outside its grant) but
  never exercised a read-only "what roles does this account hold" call —
  there's no verified ABI for that yet. Deferred to Phase 2, when EAC's read
  surface gets investigated properly instead of guessed at.
- **The endpoint takes a bare label (`device1`), not a full dotted name.** A
  node's sidecar is configured with its own tailnet (`BRAMBLE_TAILNET_NAME`,
  `BRAMBLE_TAILNET_REGISTRY`) — this is bootstrap config a node needs to
  belong to a mesh at all, not a record-schema decision (Phase 2 still owns
  record schema / wildcard-resolution discretion). Text records resolve
  across the full naming hierarchy automatically via the Universal Resolver
  (proven by Gate 0.3); `getState()` does not — it's a direct call against
  the tailnet's own subregistry contract, which is why the sidecar needs that
  address as config rather than discovering it by walking the chain.
- **Uses `viem` directly, not `ensjs`.** `04_TECH_STACK.md` pins `ensjs`, but
  Gate 0.3 — the only thing that's actually been proven against this
  deployment's custom addresses and the `UserRegistryImpl.initialize`
  signature drift — used raw `viem`. Adopting `ensjs` on top is possible
  future work, not a Section A requirement.

## Running

Requires [Bun](https://bun.sh) (TypeScript runs directly, no build step) and
`pnpm` for installs.

```
pnpm install
BRAMBLE_TAILNET_NAME=<name> BRAMBLE_TAILNET_REGISTRY=<0x...> bun run src/index.ts
```

`BRAMBLE_SIDECAR_PORT` (default `7890`) and `SEPOLIA_RPC_URL` (default a
public Sepolia RPC) are optional.

Paid relay calls additionally require `HEDERA_CLIENT_ACCOUNT_ID` and
`HEDERA_CLIENT_PRIVATE_KEY`. `HEDERA_MAX_PAYMENT_ATOMIC` is an optional
per-payment ceiling in atomic testnet USDC and defaults to `100000` (0.10
USDC). The client rejects other networks/assets and payments above this cap.

Smoke test, sidecar running standalone (no Go daemon involved — this is the
point of the HTTP boundary, per the ADR's rationale):

```
curl localhost:7890/health
curl localhost:7890/device/device1
```

## Testing

```
pnpm test
```

Reads `test/fixtures/dev-tailnet.json`, produced once by
`scripts/provision-dev-tailnet/`, and resolves it back through this sidecar's
own code — read-only, no gas, safe to run repeatedly.

## Tor client (`src/tor/`)

The sidecar runs one Tor client (tor-js 0.4.4, `wasm-file` entry,
in-memory storage) and bootstraps it at startup. Design and measurements:
`docs/adr/0013-tor-transport.md`.

- `GET /tor/status` returns `{ state, readyAt?, lastError?, message }`, where
  `state` is `starting`, `ready` or `failed`. It always answers 200.
- Until `state` is `ready`, every route except `/health` and `/tor/status`
  answers 503 `{ "error", "tor" }` with `Retry-After: 5`. `error` is
  `"starting Tor"` while starting and `"Tor failed to start: <lastError>"`
  after a failed attempt; `tor` is the same status object `/tor/status`
  returns, without `message`.
- Each bootstrap attempt has 180 s; after a failed one the next starts 30 s
  later. A bootstrap error replaces the client, once the old one's relay
  sockets are closed. A timeout keeps it, and the next attempt waits on the
  same bootstrap. `brambled` waits up to 7 minutes for `ready` at startup.
- `torFetch('ens')` and `torFetch('pay')` return a `fetch` that sends requests
  through Tor, each purpose on its own circuit. A circuit failure is retried
  once under a fresh isolation key.

Smoke test against the real Tor network (about a minute; needs outbound TCP):

```
cd sidecar
bun run scripts/tor-smoke.ts
```

It prints the bootstrap time and one request per purpose to
`https://check.torproject.org/api/ip`, and ends with `PASS` when both arrived
through Tor.
