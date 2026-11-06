# Core-Sol

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
