# Core-Sol

## ETH-227: immediate revoke after confirmed approval (demo beat 5)

Paths written: `approval/src/lib/service.ts`, `approval/src/lib/chain.ts`,
`approval/test/chain.test.ts`, `docs/reference/approval-api.md`, and this file.

Verified: the agreed HTTP Request/Response regression reproduces `409 not_granted`
with an older empty ENS snapshot after a confirmed grant. Removing that snapshot
makes the loop pass. Bypassing only the service guard exposes `502 revoke_failed`
because the admincli clear helper skips its write on the same snapshot. The fix
uses the confirmed local grant and submits an ACL clear with its own receipt.
The manual and timer regressions and approval typechecking pass. The stale-read
chain suite passes 33 tests with 130 assertions, including reverted and unresolved
writes, late grant confirmation, and duplicate-write blocking. Tests inject ENS and signer
boundaries; live RPC propagation and gateway cut-off remain Open.

Verified on 6 November 2026: `pnpm verify` passes, including both invariant checks,
formatting, deployment tests, typechecking, lint, production builds, and workspace
tests. Approval passes 120 tests with 628 assertions. No cloud unit operations or
chain writes run for this fix.

## ETH-293: concurrent rendezvous exchanges (demo beats 3 and 4)

Paths written: `relay/main.go`, `relay/concurrent_exchange_test.go`, `brambled/rendezvous/client.go`, `brambled/rendezvous/client_test.go`, `brambled/concurrent_sponsored_relay_test.go`, `docs/reference/endpoint-exchange.md`, and this file.

Verified: a real-relay regression fails at pristine `970461c` because a second gateway connection takes the first connection's offers. Pair-scoped registrations pass that regression with real WireGuard and HTTP 200. Registry admission is a fixture. Live public-device acceptance remains Open; no runtime or ACL change ran for this code-only fix.

## ETH-181: device onboarding (demo beats 2 and 5)

Recovered Core-Claude commit `37862fc` in branch `core-sol/eth-181-join-api`.

Paths written:

- `admincli/src/{lib.ts,lib.test.ts,enroll.ts,enroll-chain-viem.ts,enroll-memory.ts}`
- `approval/src/lib/{api.ts,chain-env.ts,contracts.ts,validation.ts,joins.ts,join-chain.ts}`
- `approval/src/app/api/[...path]/route.ts`
- `approval/test/joins.test.ts`
- `docs/ai-usage/core-sol.md`

Verified: HTTP lifecycle tests use the admincli library and in-memory chain; no testnet writes in these tests.

Additional paths written:

- `approval/src/components/{approval-console.tsx,join-console.tsx}`
- `approval/e2e/{console.spec.ts,memory-server.ts}`
- `approval/{package.json,tsconfig.json,next.config.ts,playwright.config.ts,.env.example}`
- `approval/test/node-config.test.ts`
- `approval/scripts/smoke-join.ts`
- `admincli/src/enroll-chain-viem.test.ts`
- `join-ui/{package.json,tsconfig.json,src/index.tsx}`
- `pnpm-workspace.yaml`, `pnpm-lock.yaml`
- `deploy/scripts/gen-node-config.mjs` (reused unchanged from install-hosting work)
- `scripts/print-node-config.mjs`
- `docs/reference/approval-api.md`
- `README.md`

Open: real Sepolia smoke requires administrator configuration; this run lacks `APPROVAL_ENS_PARENT`.

Verified on 6 November 2026: `pnpm verify` passes (approval: 90 tests, 516 assertions). Browser checks pass in desktop and mobile layouts (8 tests), including join visibility under 2 seconds with the in-memory chain. Browser command uses `PLAYWRIGHT_BROWSERS_PATH=/home/anonjr/.cache/ms-playwright BRAMBLE_TEST_UI_PORT=13181 BRAMBLE_TEST_API_PORT=13182 pnpm --filter @bramble/approval test:ui` because the normal dev port is occupied.

## ETH-287: demo rehearsal harness

Paths written:

- `deploy/scripts/rehearse.py`
- `deploy/REHEARSAL.md`
- `docs/ai-usage/core-sol.md`

Demo beats: 1 (denied), 2 (request and approve), 3 (healthy call, rendezvous payment,
new HCS grant record), and 5 (revoke and measured denial).

Verified: offline simulation and local fault checks. Open: ETH-244 owns execution
against the existing shared cloud pair. This task does not start nodes or use the
shared ACL during local verification.

## ETH-319: publish the reviewed demo corrections (demo beats 1, 2, 3 and 5)

Recovered Keystone's saved correction commits through `6e651b3` in an isolated
Core-Sol worktree. The operator authorized one follow-up PR, one QA-Sol review
and merge into `hackathon`.

Paths written: `docs/ai-usage/core-sol.md`. The recovered paths remain recorded
in `docs/ai-usage/keystone.md`. Live browser and public-route Pi acceptance remain
Open in ETH-313; publication does not use the cloud nodes.
