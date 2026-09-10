# Gate 0.4 — Hedera x402 through Blocky402

Standalone proof for `docs/10_DAY0_GATES.md` Gate 0.4: one trivial paid request, end to end, settled through Blocky402 on Hedera testnet, transaction confirmed on-chain. Deliberately not integrated with `relay/` — see `docs/05_BUILD_PLAN.md` Phase 4 Section A. Kept outside the pnpm/Turborepo workspace, same precedent as `scripts/gate0.3-eac-check/`.

## What it proves

- `server.mjs` — a Hono route (`GET /paid-ping`) gated by `@x402/hono`'s `paymentMiddleware`, priced at 0.01 HBAR, verified/settled through Blocky402's hosted testnet facilitator (`https://api.testnet.blocky402.com`) rather than a local facilitator.
- `client.mjs` — calls the route once via `@x402/fetch`'s `wrapFetchWithPaymentFromConfig`, which transparently handles the 402 → sign → retry flow using a real Hedera ECDSA key (`@x402/hedera`'s `ExactHederaScheme` + `createClientHederaSigner`).
- Settles in **native HBAR** (`asset: "0.0.0"`), not an HTS token — sidesteps the `TOKEN_NOT_ASSOCIATED_TO_ACCOUNT` failure mode `@x402/hedera`'s own README documents, since this script only needs to prove the payment rail works, not exercise token settlement.

## Verified run (Sept 9, 2026)

```
$ node client.mjs
Calling http://localhost:4402/paid-ping as 0.0.10433667 ...
HTTP 200 { pong: true, at: '2026-09-09T07:21:26.748Z' }

Settlement: {
  success: true,
  payer: '0.0.10433667',
  transaction: '0.0.7162784@1788938479.675435198',
  network: 'hedera:testnet'
}
```

Confirmed independently against the public mirror node (not just trusting the client's own report):

```
$ curl -s https://testnet.mirrornode.hedera.com/api/v1/transactions/0.0.7162784-1788938479-675435198
CRYPTOTRANSFER SUCCESS
    0.0.802       253002    (network fee, paid by the facilitator's fee-payer account)
    0.0.7162784  -253002
    0.0.10433667 -1000000   (client/payer — this project's account)
    0.0.10433704  1000000   (server/payee — this project's account)
```

HashScan: https://hashscan.io/testnet/transaction/0.0.7162784@1788938479.675435198

**Gate 0.4: ✅ VERIFIED.** Real HTTP 402 challenge, real signed Hedera transaction, real on-chain settlement, independently confirmed via the mirror node.

## Running it yourself

```bash
npm install
npm run generate-accounts     # writes .env with two fresh ECDSA keypairs + EVM addresses
# fund both EVM addresses at https://portal.hedera.com/faucet (100 HBAR each, no login)
npm run resolve-accounts      # resolves the funded EVM addresses to 0.0.X account IDs
npm run server                # terminal 1
npm run client                # terminal 2
```

`.env` is git-ignored (repo-root `.gitignore` covers `.env` at any depth) — the generated keys never leave this machine.

## Findings that resolved open questions elsewhere

- **Blocky402-vs-Scaffold-HBAR-starter-kit question (`docs/11_SOURCE_NOTES.md`) is moot for this proof**: this script calls Blocky402's own hosted testnet facilitator directly (`api.testnet.blocky402.com`, confirmed live via its `/supported` endpoint, which lists `hedera:testnet`), never going through the Scaffold-HBAR starter at all.
- **`@x402/core` / `@x402/hedera` v2 package surface, previously unverified in `04_TECH_STACK.md`**: confirmed real, published, and usable as documented in their own bundled READMEs (checked against the installed packages' actual `.d.ts` files, not just the docs site). `@x402/fetch` and `@x402/hono` (also real, published) turned out to be the right layer to build on — much less code than hand-rolling the low-level `x402ResourceServer`/`x402HTTPClient` wiring.
- **New dependency**: `@hiero-ledger/sdk` (Hedera's own SDK; `@x402/hedera` re-exports a pinned subset of it — importing it directly alongside `@x402/hedera` in a workspace risks duplicate installs, per that package's own README).
- **Settlement asset options on `hedera:testnet`**: native HBAR (`0.0.0`, 8 decimals, tinybars) or testnet USDC (`0.0.429274`, 6 decimals, HTS — requires token association for both parties first). This proof uses HBAR.
- **Client-side spend controls**: `@x402/core`'s default spend-control allowlist only permits assets its `findDefaultAsset` recognizes as "default" for a network; native HBAR on Hedera wasn't recognized as one out of the box, so this script disables spend controls (`spendControls: false`) rather than guess at the correct allowlist shape. Worth revisiting properly (an explicit `allowedAssets` entry) before any code that isn't a throwaway proof reuses this pattern.
