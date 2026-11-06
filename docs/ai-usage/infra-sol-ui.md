# Infra-Sol-UI assistance

## ETH-47: HCS mirror verification panel (beat 3)

Infra-Sol-UI implemented `approval/src/lib/audit-verification.ts`, `approval/src/lib/audit-fixture.ts`, `approval/src/fixtures/hcs-audit.json`, `approval/src/components/hcs-panel.tsx`, `approval/src/app/audit/fixtures/page.tsx`, `approval/test/audit-verification.test.ts`, and `approval/e2e/audit.spec.ts`. It changed `approval/src/lib/api.ts`, `approval/src/components/approval-console.tsx`, `approval/src/app/globals.css`, `approval/tsconfig.json`, `agent-mcp/tsconfig.json`, and `approval/README.md`.

The agent used the merged HCS record format and verifier. A Python standard-library script independently calculated the synthetic HMAC fixtures. No signing, keys, mainnet activity or live Hedera submission was involved. Humans retain the H4 screenshot approval and design checkpoints.

## ETH-270 — registry discovery deployment blocks

Demo beats 2, 3 and 4. Paths written:

- `deploy/scripts/gen-node-config.mjs`
- `deploy/scripts/gen-node-config.test.mjs`
- `deploy/env/public.env.example`
- `deploy/README.md`
- `docs/ai-usage/infra-sol-ui.md`

Verified red then green at the existing public manifest generator seam with both cloud deployment blocks set to `12074600`. Invalid or inexact deployment blocks fail generation. Unset optional fields remain omitted.

## ETH-251: demo scripts and rehearsal (beats 1, 2, 3 and 5)

- Wrote `docs/DEMO_SCRIPT.md`: three- and six-minute scripts, sponsor narration, rehearsal, recovery and conditional installer acceptance. Read repository sources and ETH-205/ETH-244 timing comments; did not run cloud lifecycle actions or inspect secrets.
- Appended `docs/ai-usage/infra-sol-ui.md` to record these paths.

- Addressed round-1 findings in `docs/DEMO_SCRIPT.md`: full-lifecycle warmup evidence, more operating time with a pre-created pending request, action-relative stall deadlines, Tor attribution, preflight probes, installer link and rehearsal/startup cost. Appended this usage record. No live rehearsal or cloud changes.

## ETH-304: PR #51 conflict resolution (beats 2, 3 and 4)

Merged `hackathon` at `70fbfb7ef59a99bac3d52c9b204f3ad2b0383997` into the author branch. Resolved only `docs/ai-usage/infra-sol-ui.md`, retaining both ETH-270 and ETH-251 records. The manifest generator, tests, public template and deployment guide retain their reviewed source. No cloud operation or live acceptance is part of this change. Current-head review remains required.
