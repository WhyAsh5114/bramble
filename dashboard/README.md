# bramble dashboard

A read-only view over one bramble tailnet — devices, ACL state, and relay
economics, in one screen with links to Sepolia Etherscan and the hackathon's
ENSv2 explorer. Built as its own standalone package deliberately: it never
imports `brambled`/`sidecar`/`admincli` code, only talks to an already-running
`sidecar` over HTTP.

## What this is not

It never signs or sends a transaction. Everything here is a `GET` against
state that `sidecar` (and, for data-relay prices, `relay-sidecar`) already
computes from on-chain reads. No wallet connection, no private key, nothing
write-path. See `next.config.ts` and `src/app/api/` for the full extent of
what it talks to.

## Running it

1. Have a `brambled serve` node's `sidecar` already running (it starts
   automatically as part of `brambled serve` — see `brambled/README.md`).
2. `cp .env.local.example .env.local` and set `SIDECAR_URL` (default assumes
   the sidecar is on the same machine at its default port, 7890) and
   `DEVICE_LABELS` (comma-separated ENS labels to watch).
3. From the repository root, run `pnpm install --frozen-lockfile` once, then
   `pnpm dev:dashboard` and open `http://localhost:3000`.

## Architecture

- `sidecar` binds to loopback only and sets no CORS headers (deliberately —
  see `sidecar/src/index.ts`), so the browser can't call it directly from a
  different origin. Explicit GET-only Route Handlers proxy just `/health`,
  `/relays`, and `/device/:label`; the sidecar's payment routes are not
  reachable through the dashboard.
- Data-relay prices come from each relay's own `relay-sidecar`, discovered at
  runtime from `sidecar`'s `/relays`. The price route accepts only a URL in
  that discovered set, fetches only `/price`, rejects redirects, and times
  out after five seconds.
- Everything on screen is client-side polled (see `src/hooks/use-polling.ts`)
  against those two proxy surfaces. No `wagmi`, no direct-from-browser chain
  reads — the hackathon deployment's Universal Resolver override
  (`docs/04_TECH_STACK.md`) only exists in `sidecar`'s viem client, and this
  dashboard is not a second place that needs to get it right.

## Known gaps

- No live peer/handshake state (WireGuard connection status, rx/tx bytes,
  gateway CONNECT allow/deny events) — that data only exists in `brambled`'s
  own memory today, and `brambled` has no HTTP surface at all yet. Adding one
  is real, separate scope (touches `brambled/main.go`) — see the project's
  build-plan discussion before doing it, since another change may already be
  in flight against that file.
- No transaction history (enrollment, revocation, role grants, past relay
  payments) — `admincli`'s scripts print these once and exit; nothing
  persists them for a dashboard to read back. Etherscan/HashScan links here
  only cover state that's queryable _right now_ (current pubkey/expiry/ACL,
  current relay prices), not history.
