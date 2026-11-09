# Infra-Sol-UI assistance

## ETH-47: HCS mirror verification panel (beat 3)

Infra-Sol-UI implemented `approval/src/lib/audit-verification.ts`, `approval/src/lib/audit-fixture.ts`, `approval/src/fixtures/hcs-audit.json`, `approval/src/components/hcs-panel.tsx`, `approval/src/app/audit/fixtures/page.tsx`, `approval/test/audit-verification.test.ts`, and `approval/e2e/audit.spec.ts`. It changed `approval/src/lib/api.ts`, `approval/src/components/approval-console.tsx`, `approval/src/app/globals.css`, `approval/tsconfig.json`, `agent-mcp/tsconfig.json`, and `approval/README.md`.

The agent used the merged HCS record format and verifier. A Python standard-library script independently calculated the synthetic HMAC fixtures. No signing, keys, mainnet activity or live Hedera submission was involved. Humans retain the H4 screenshot approval and design checkpoints.

## ETH-251: demo scripts and rehearsal (beats 1, 2, 3 and 5)

- Wrote `docs/DEMO_SCRIPT.md`: three- and six-minute scripts, sponsor narration, rehearsal, recovery and conditional installer acceptance. Read repository sources and ETH-205/ETH-244 timing comments; did not run cloud lifecycle actions or inspect secrets.
- Appended `docs/ai-usage/infra-sol-ui.md` to record these paths.

- Addressed round-1 findings in `docs/DEMO_SCRIPT.md`: full-lifecycle warmup evidence, more operating time with a pre-created pending request, action-relative stall deadlines, Tor attribution, preflight probes, installer link and rehearsal/startup cost. Appended this usage record. No live rehearsal or cloud changes.

## ETH-244: post-fix validation (beats 1, 2, 3 and 5)

- Built `release/bramble_{linux,darwin}_{amd64,arm64}.tar.gz`, `release/SHA256SUMS`, and embedded `node-daemon/ui/dist` from `970461c`. Used the existing corrected release driver and installer from the earlier validation branch. Published to the established console host and verified all four downloaded archive hashes.
- Wrote validation drivers, `paid-capture.ts`, sanitized evidence and reports under `/home/anonjr/bramble-work/status/eth-244/postfix-*` and `postfix-public-device/`. The private device home is excluded from deliverables. Wrote a temporary `/opt/bramble/sidecar/.eth276-capture.ts` on the gateway and removed it after one bounded Tor negotiation.
- Updated Paperclip reports for ETH-244 and ETH-276. The payment attempt returned HTTP 200; the historical rejection cause remains Open. Credentials were passed through the existing runtime configuration to their consumers without inspection or output.
- Updated this usage record. No application source changed.
