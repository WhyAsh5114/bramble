# Core-Claude AI usage

## 2026-11-06: ETH-180 PR #38, round 1 hand-over

Demo beats: 2 (request and approve) and 4 (relay fallback).

Took over PR #38 at `88d643d`. Infra-Claude's `8ea304c` already fixed
QA-Sol's four spec findings and two standards findings. I read the fix
commit and did not rewrite it. I merged the current PR #35 head (`8ba4096`,
five review-fix commits newer than the earlier merge) so the launcher is
checked against the brambled that will land. The merge had no conflicts and
changed no brambled flag.

Verified:

- `pnpm verify`, `pnpm test:access-flow`, `pnpm check:no-endpoints` and
  `pnpm check:no-local-state` passed.
- `cd node-daemon && go vet ./... && go test -race ./...` and
  `cd brambled && go test -race ./statusapi` passed.
- The built brambled accepts `serve -sponsored-relay -relay-first -label
  flag-check` in an empty environment. It stops at the expected
  missing-tailnet error instead of rejecting the flags.

Read from source: the only file the launcher writes logs to is
`logs/bramble.log` (detached mode). It receives the daemon's own lines and
the filtered brambled exit summary, never raw child output.

Open: the real-Sepolia, relay-only acceptance run (`connected`, then
`/api/test` ok through the cloud gateway).

Paths written:

- `docs/ai-usage/core-claude.md`

## ETH-217: merge `hackathon` into PR #38

Merge-conflict resolution (AI-assisted): `brambled/main.go` (the hackathon `startAdoptWatcher` refactor now passes `payer` and `adoptPace`), `README.md` (status table: the W5 rows merged, ETH-177 T6 and ETH-177e rows kept), `.gitignore`, `docs/ai-usage/net-claude.md` (both sides kept), `pnpm-lock.yaml` (regenerated with `pnpm install`).

## ETH-47: HCS panel merge and stale-label fix (beat 3)

Core-Claude merged `hackathon` into the HCS panel branch, resolving `agent-mcp/tsconfig.json`, `approval/README.md` and `approval/src/lib/api.ts`. In `api.ts` the verification route now reads the service's own `AuditRecorder` unless `auditSource` overrides it. It changed `approval/src/components/hcs-panel.tsx` so a failed refresh no longer keeps the verified detail line, extended `approval/e2e/audit.spec.ts` and `approval/test/audit-verification.test.ts`, and updated `approval/README.md`. It ran `pnpm verify` and the browser tests; no signing, keys or live Hedera activity were involved.

## ETH-241: merge `hackathon` into PR #28

Merge-conflict resolution (AI-assisted): `docs/ai-usage/core-claude.md` and `docs/ai-usage/infra-claude.md` (both sides kept). No code conflicts.

## ETH-181: merge `hackathon` into PR #41 (beats 2 and 5)

Merge-conflict resolution (AI-assisted): `README.md` (hackathon rows kept, W1 row points at PR #41), `pnpm-workspace.yaml` (`join-ui` and `local-ui`), `approval/src/lib/api.ts` (audit verification route and the join routes), `approval/src/components/approval-console.tsx` (activity-feed layout with `JoinConsole` added to the main column), `approval/e2e/memory-server.ts` (activity sources and the in-memory join chain; ENS parent `bramble.eth` for both), `approval/e2e/console.spec.ts` (join tailnet `bramble.eth`), `pnpm-lock.yaml` (regenerated with `pnpm install`). It ran `pnpm verify` and the approval browser tests; no signing, keys or chain writes were involved.

## ETH-181: PR #41 review round 1 fix (beats 2 and 5)

AI-assisted (Core-Claude): `admincli/src/enroll-chain-viem.ts` (a write replaced by a cancellation or a different transaction is no longer counted as confirmed; a repriced copy returns the hash that was mined), `admincli/src/enroll-chain-viem.test.ts` (cancelled, replaced and repriced regressions), `approval/test/joins.test.ts` (a cancelled revoke through the API is not reported as revoked). Tests use a loopback RPC; no signing, keys or chain writes were involved.

## Earlier entries from `hackathon`

- ETH-35 (live activity feed, beats 1, 3 and 5): `approval/src/lib/activity.ts`, `approval/src/lib/explorer.ts`, `approval/src/lib/payments-handler.ts`, `approval/src/hooks/use-activity.ts`, `approval/src/components/activity-feed.tsx`, `approval/src/components/approval-console.tsx`, `approval/src/components/request-card.tsx`, `approval/src/lib/{api,service,contracts,console-api}.ts`, `approval/src/app/api/{[...path],payments}/route.ts`, `approval/src/app/globals.css`, `approval/test/activity.test.ts`, `approval/test/fixtures/activity-status.json`, `approval/e2e/{console.spec.ts,memory-server.ts}`, `approval/README.md`, `brambled/statusapi/{server.go,server_test.go,activity_contract_test.go}`, `dashboard/README.md`, `docs/how-to-test-approval-console.md` and this file. AI-assisted (Core-Claude). The feed adds read-only HTTP reads and no write path. Human H4 screenshot approval and a live two-machine cut-off measurement are still required.
- ETH-223 (PR #24 review round 1 fixes): `approval/src/lib/activity.ts`, `approval/src/lib/api.ts`, `approval/test/activity.test.ts`, `approval/e2e/memory-server.ts`, `approval/README.md`, `docs/how-to-test-approval-console.md` and this file. AI-assisted (Core-Claude).

## ETH-294: gateway status forward for the console activity feed (beats 1, 3 and 5)

AI-assisted (Core-Claude): `deploy/systemd/user/bramble-gateway-status-tunnel.service` (new user unit: admin loopback `17899` to gateway loopback `7899` over the `bramble-gateway` SSH alias), `deploy/ACTIVITY-SOURCES.md`, `deploy/APPROVAL-USER-SERVICE.md`, `deploy/env/admin.env.example` and this file. Read-only checks only: a temporary forward fetched gateway `/activity`; no service restart, environment edit, grant, revoke or payment.
