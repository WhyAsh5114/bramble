# Hedera-Sol AI usage

## 2026-11-06 — ETH-180 round-1 fix verification

Demo beats: 2 (request and approve) and 4 (relay fallback).

Continued PR #38 from `8ea304c` in an isolated worktree. Infra-Claude's
existing commit contains all four spec fixes and both documentation fixes
from QA-Sol's first review. Reused those changes without rewriting them.

Verified:

- `pnpm verify` passed, including cached Turbo tasks.
- `pnpm test:access-flow` passed with an uncached Go access test and MCP
  lifecycle test. This exercises in-memory ENS and loopback WireGuard.
- `cd node-daemon && go test -race ./...` passed.
- The built `brambled` accepts `serve -sponsored-relay -relay-first -label
review-device` with an isolated environment. It reaches the expected
  missing-tailnet configuration error instead of rejecting the flags.

Read from source: child output stays in memory; the crash summary excludes
admission, gateway and authorization lines. Leave invalidates late join
responses. The launcher reads ENS-discovered relay hosts from `/relays`.
README W5 and the invalid-config reference now describe the implementation.

Open: the mock-join-API plus real-Sepolia relay-only acceptance run. Local
checks do not establish `connected` or a successful `/api/test` against the
cloud gateway. PR #35 is still open and remains the merge dependency.

Paths written:

- `docs/ai-usage/hedera-sol.md`

## ETH-243 Pi launcher continuation

Demo beats: 1, 2, 3 and 5. Continued PR #42 from `7c0b8fe` in an isolated worktree.

Paths written:

- `deploy/scripts/pi-agent.sh`
- `agent-mcp/test/pi-launcher.test.ts`
- `agent-mcp/PI.md`
- `agent-mcp/PI_SECURITY.md`
- `agent-mcp/REFERENCE.md`
- `deploy/README.md`
- `docs/ai-usage/hedera-sol.md`

The launcher check reproduced the overwritten skill-path failure before the fix.
Verified on 6 November 2026: both focused launcher tests, shell syntax checks,
`git diff --check`, and `pnpm verify` pass (34 agent-mcp tests).
Clean worker sequence remains Open pending the ETH-244 node handoff.

## ETH-243 isolated cloud attempt

Demo beats: 1, 2, 3 and 5. Verified the stopped-node handoff, deployed the
reviewed launcher and workflow, and ran the initial deny call and Pi prompt.
The gateway confirmed a missing grant. Pi requested `task`; the operator POST
failed with HTTP 401. No grant was approved. Both nodes, the Pi process and its
MCP child, and the worker approval tunnel were stopped. Two full runs remain Open.

Paths written:

- `agent-mcp/PI.md`
- `deploy/README.md`
- `docs/ai-usage/hedera-sol.md`
- `/home/anonjr/bramble-work/status/eth-243/isolated-w2.py`
- `/home/anonjr/bramble-work/status/eth-243/isolated-w2/` (timings and Pi output)
- `/home/anonjr/bramble-work/status/eth-243-isolated-docs-verify.log`
- Worker: `/home/agent/bin/pi-agent`
- Worker: `/home/agent/.pi/agent/skills/bramble-private-service/SKILL.md`

## ETH-243 authentication check after operator release

Demo beats: 2, 3 and 5. Verified both node services inactive with MainPID 0.
Reopened only the worker approval tunnel, confirmed the same pending request,
and reproduced HTTP 401 on a protected GET and a nonexistent POST with the
authorized worker file passed directly into the HTTP header. Neither node
service was started. No request, grant, credential or service configuration
changed. Closed the temporary tunnel and verified its listener absent.

Paths written:

- `agent-mcp/PI.md`
- `deploy/README.md`
- `docs/ai-usage/hedera-sol.md`

Additional local evidence paths written in this wake:

- `/home/anonjr/bramble-work/status/eth-243-release-auth-verify.log`
- `/home/anonjr/bramble-work/status/eth-243-release-auth-receipts.json`
- `/home/anonjr/.cache/agent-tmp/eth-243-post-release-handoff.md` (handoff references the saved task report)

## ETH-243 running-slot validation

Demo beats 1, 2, 3 and 5. Verified two isolated Pi access-lifecycle runs at
15:51–15:57 UTC on 6 November 2026. Full beat 3 payment/feed acceptance stays
Open on ETH-294. Both requests are revoked; Pi/MCP and the temporary tunnel
are gone. The running pair is handed to ETH-271 for measurements and final stop.

Paths written:

- `agent-mcp/PI.md`
- `agent-mcp/README.md`
- `deploy/README.md`
- `docs/ai-usage/hedera-sol.md`
- `/home/anonjr/bramble-work/status/eth-243/running-slot.py`
- `/home/anonjr/bramble-work/status/eth-243/summarize-running-slot.py`
- `/home/anonjr/bramble-work/status/eth-243/running-slot/` (transcripts, timings, receipts, feed metadata)
- `/home/anonjr/bramble-work/status/eth-243/running-slot-evidence.tar.gz`
- `/home/anonjr/bramble-work/status/eth-243-running-slot-verify.log`

No launcher, MCP implementation, tests, node configuration, or credentials change.
Round 2 remains bound to `91f7ca4`; merge-captain disposition covers later docs.

Additional handoff paths:

- `/home/anonjr/bramble-work/status/eth-243/artifact-helper/` (upload receipts)
- `/home/anonjr/.cache/agent-tmp/eth-243-running-slot-handoff.md`

## ETH-308 PR 42 documentation conflict resolution

Demo beats: 1, 2, 3 and 5. Merge `origin/hackathon` at `70fbfb7` into
PR 42 with a merge commit. Preserve the Pi runtime, lifecycle measurements,
Claude Code token isolation guidance and approval authority limits. Resolve
conflicts only in the two agent MCP documentation files.

Paths written:

- `agent-mcp/README.md`
- `agent-mcp/REFERENCE.md`
- `docs/ai-usage/hedera-sol.md`

Read from source: the incoming branch changes `deploy/scripts/gen-node-key.sh`
and adds `gen-node-key.test.sh`, `join-test.py` and `rehearse.py`. These inherited
changes make the requested whole-directory diff against `91f7ca4` nonempty.
The conflict resolution does not edit scripts, tests, launchers or SKILL files.
The approved Pi files remain byte-identical to `91f7ca4`.

## 2026-11-06 — ETH-184 documentation closeout

Demo beats: 1–5. Reconciled recorded installer, enrollment, cloud access and
rehearsal evidence from ETH-179, ETH-250, ETH-244 and ETH-287 under the
operator's docs-only direction and cross-family review waiver. No live test,
cloud mutation, approval, payment or browser walkthrough ran for this change.
The acceptance log attributes reported observations and preserves the fresh-device
connectivity failure and other Open outcomes.

Verified: `pnpm verify` exits 0 (Turbo tasks cached), `git diff --check` is clean,
and relative file links in all three deliverables resolve. These repository checks
do not add live acceptance evidence.

Paths written:

- `README.md`
- `docs/DEMO_SCRIPT.md`
- `docs/measurements/2026-11-06-onboarding-acceptance.md`
- `docs/ai-usage/hedera-sol.md`

## 2026-11-06 — ETH-317 admin console overhaul

Demo beats: 2, 4 and 5. Adds an authenticated, redacted network view and
tracked-device and configured-relay diagnostics. Preserves the decision services.

Paths written in the API slice:

- `approval/src/lib/network.ts`
- `approval/src/lib/api.ts`
- `approval/src/lib/activity.ts`
- `approval/src/app/api/[...path]/route.ts`
- `approval/test/network.test.ts`
- `approval/test/api.test.ts`
- `docs/ai-usage/hedera-sol.md`

Verified: API authentication, topology redaction and grant-state tests fail before
implementation and pass after implementation. Browser and repository validation
follow in the UI slice.

Paths written in the UI and evidence slice:

- `approval/src/app/globals.css`
- `approval/src/app/layout.tsx`
- `approval/src/app/audit/fixtures/page.tsx`
- `approval/src/app/payments/page.tsx`
- `approval/src/components/approval-console.tsx`
- `approval/src/components/join-console.tsx`
- `approval/src/components/request-card.tsx`
- `approval/src/components/network-panel.tsx`
- `approval/e2e/console.spec.ts`
- `approval/e2e/memory-server.ts`
- `approval/e2e/network.spec.ts`
- `approval/e2e/accessibility.spec.ts`
- `approval/package.json`
- `join-ui/src/index.tsx`
- `pnpm-lock.yaml`
- `README.md`
- `approval/README.md`
- `docs/reference/approval-api.md`
- `docs/reference/approval-network.md`

The screenshot check caught repeated onboarding panels from duplicate sibling
keys. A browser count assertion reproduces the defect before the key correction.

Implementation review: Standards found no documented violation and one optional
duplicated-validator smell. Spec found numeric peer endpoints were misclassified
against DNS-named relays. A failing API test reproduces that issue; bounded
server-side DNS resolution fixes it and keeps unresolved paths unknown.
The network module also avoids importing browser fetch types into server consumers.

## 2026-11-06 — ETH-317 requested no-feed revision

Demo beats: 2, 4 and 5. Retains the same branch and PR #66. Builds topology from
approval facts without the gateway tunnel and probes only server-known HTTP
targets from the console host. Missing device probe targets remain unavailable.

Paths written in the API revision:

- `approval/src/lib/network.ts`
- `approval/src/lib/api.ts`
- `approval/src/app/api/[...path]/route.ts`
- `approval/test/network.test.ts`
- `approval/test/api.test.ts`
- `docs/ai-usage/hedera-sol.md`

Verified: the no-feed API regression fails before the revision and passes after
it with `network` omitted and `BRAMBLED_STATUS_URL` unset. Seven network API tests
pass, including real loopback HTTP checks, grant/revoke state and response redaction.

Paths written in the browser and documentation revision:

- `approval/src/app/globals.css`
- `approval/src/components/network-panel.tsx`
- `approval/e2e/memory-server.ts`
- `approval/e2e/network.spec.ts`
- `docs/reference/approval-network.md`
- `docs/reference/approval-api.md`
- `approval/README.md`
- `README.md`

The no-feed browser assertion reproduces absent unavailable-field labels against
the prior build. New browser screenshots and axe results follow final validation.

Verified on 6 November 2026 after the revision: `pnpm verify` exits 0;
126 approval tests pass; all 24 Playwright tests pass. Eight axe scans report zero
violations. SVG color contrast remains incomplete for 12 text nodes in each live
fixture scan and eight in each no-feed scan; explicit palette contrast checks
pass. Inspected all four no-feed screenshots at desktop and phone widths in both
themes. Automated checks do not establish complete manual WCAG conformance or
live cloud connectivity.

Read from source: implementation review finds zero documented standards
violations and zero spec gaps. One optional duplicated-defaults smell remains in
unavailable device construction. Grant/revoke service files remain unchanged.
Cross-family review and merge remain Open.

Evidence paths written:

- `/home/anonjr/bramble-work/status/eth-317-revision-verify.log`
- `/home/anonjr/bramble-work/status/eth-317-revision-browser.log`
- `/home/anonjr/bramble-work/status/eth-317-revision-evidence/`
- `approval/test-results/` (ignored screenshots and axe reports)

- `/home/anonjr/.cache/agent-tmp/eth-317-revision-handoff.md`

## ETH-325 — console branding

Read from source: this visual change serves demo beats 1–5. The approval console,
local UI and dashboard use the existing landing logo as their header mark and
favicon. The join UI inherits branding from its approval and local UI hosts.
The light palette uses the landing green; PR #66's dark palette, control borders,
focus outlines and semantic warning colours stay in place.

Paths written:

- `approval/public/bramble-logo.png`
- `approval/src/app/globals.css`
- `approval/src/app/layout.tsx`
- `approval/src/app/payments/page.tsx`
- `approval/src/components/approval-console.tsx`
- `local-ui/public/bramble-logo.png`
- `local-ui/src/app/globals.css`
- `local-ui/src/app/layout.tsx`
- `local-ui/src/components/local-app.tsx`
- `dashboard/public/bramble-logo.png`
- `dashboard/src/app/globals.css`
- `dashboard/src/app/layout.tsx`
- `dashboard/src/components/nav.tsx`
- `dashboard/src/app/favicon.ico` (removed the starter icon)
- `docs/ai-usage/hedera-sol.md`

Validation: Open until the checks below run.
