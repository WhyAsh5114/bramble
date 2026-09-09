# relay-sidecar

The rendezvous relay's payment surface (`docs/adr/0007-rendezvous-relay-metering.md`, Phase 4 Section B). Mints a self-contained, HMAC-signed rendezvous token after a real x402 payment settles through Blocky402 on Hedera testnet — the Go relay (`relay/`, `-meter`) verifies that token entirely locally, no callback to this process on the per-connection path.

Spawned automatically by `relay -meter` as a child process (`relay/sidecar.go`) — not normally run standalone.

## What it proves

- `POST /rendezvous-token`, x402-gated via `@x402/hono`, priced at 0.01 testnet USDC (`0.0.429274`), settled through Blocky402's hosted facilitator — same rail Gate 0.4 proved, now actually wired into the relay instead of a standalone script.
- Token minting is a pure local operation after settlement (`src/token.ts`) — the Go relay's verifier (`relay/token.go`) implements the exact same HMAC scheme and never calls back over HTTP.

## Live run (Sept 9, 2026)

Both Gate 0.4's accounts (`scripts/gate0.4-blocky402-check/.env`) reused here — associated with testnet USDC via `associate-usdc.mjs`, payer funded from Circle's testnet faucet.

Started the real metered relay, which spawned this sidecar itself:

```
$ ./bin/relay -addr :9420 -meter -payee 0.0.10433704 -sidecar-dir ../relay-sidecar -sidecar-port 7891
rendezvous relay listening on [::]:9420
starting relay-sidecar...
relay-sidecar listening on :7891
metering enabled: rendezvous-token required, relay-sidecar on :7891
```

Called `sidecar/src/payments/client.ts`'s `payForRendezvousToken()` directly (the exact code the ENS sidecar's `/rendezvous-token` route runs) twice, to get one real token per side of a two-sided exchange:

```
tx A: 0.0.7162784@1788941365.961612782
tx B: 0.0.7162784@1788941368.403212861
```

Then ran a real bidirectional `brambled/rendezvous.Exchange` against the live metered relay using those two real tokens:

```
bob:   candidate="203.0.113.1:51820" err=<nil>
alice: candidate="203.0.113.2:51820" err=<nil>
```

Both sides learned the other's candidate — a metered connection attempt completing exactly like an unmetered one, proving the token minted by this real sidecar process is accepted by the real Go relay's independent verifier (not just the in-process test stub in `relay/main_test.go`).

Confirmed independently against the mirror node (not just the client's own report) — a real USDC transfer, not HBAR:

```
$ curl -s https://testnet.mirrornode.hedera.com/api/v1/transactions/0.0.7162784-1788941253-045333577
CRYPTOTRANSFER SUCCESS
   token 0.0.429274 0.0.10433667 -10000   (client/payer)
   token 0.0.429274 0.0.10433704  10000   (relay operator/payee)
```

HashScan: https://hashscan.io/testnet/transaction/0.0.7162784@1788941365.961612782

**Gate 4.1: ✅ satisfied.** A real connection attempt through the actual rendezvous relay settled a real, on-chain, metered payment — not a standalone proof, the relay's own live payment path.

## Running it yourself

```bash
bun install
```

Then start the relay with `-meter` (see `relay/README.md`'s flags) — it spawns this sidecar automatically. To call this sidecar's payment route directly instead (e.g. for testing), see `sidecar/src/payments/client.ts`'s `payForRendezvousToken()`, which needs `BRAMBLE_RENDEZVOUS_PAYMENT_URL`, `HEDERA_CLIENT_ACCOUNT_ID`, `HEDERA_CLIENT_PRIVATE_KEY` in its environment.
