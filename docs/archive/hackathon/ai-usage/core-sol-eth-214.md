# Core-Sol: ETH-214

Demo beats: 2 (request and approve), 5 (revoke).

Paths written:

- `approval/src/lib/service.ts`
- `deploy/systemd/user/bramble-approval.service`
- `deploy/systemd/user/bramble-console-tunnel.service`
- `deploy/env/admin.env.example`
- `deploy/APPROVAL-USER-SERVICE.md`
- `docs/ai-usage/core-sol-eth-214.md`

Signer logging reuses the identical change proposed by Infra-Claude in PR #39.
Runtime configuration stays outside git under the admin user's persistent home.

Operational files written outside git:

- `/home/anonjr/.config/bramble/approval.env` (authorized migration; secret values stay private)
- `/home/anonjr/.config/systemd/user/bramble-approval.service`
- `/home/anonjr/.config/systemd/user/bramble-console-tunnel.service`
- `/home/anonjr/bramble-work/approval-runtime/` (independent clone and build)
- `/home/anonjr/bramble-work/status/eth-214-verify.log`
- `/home/anonjr/bramble-work/status/eth-214-runtime-build.log`
- `/home/anonjr/bramble-work/status/eth-214-public-verification.json`
- `/home/anonjr/bramble-work/status/eth-214-console-revoked.png`
- `/home/anonjr/bramble-work/status/eth-214-report.md`
- `/home/anonjr/.cache/agent-tmp/core-sol-eth-214-handoff.md` (review handoff)

## ETH-234: merge conflict resolution

Demo beats: 3 (works and pays), 5 (revoke).

Paths written:

- `approval/src/lib/service.ts`
- `docs/ai-usage/core-sol-eth-214.md`

Merge `origin/hackathon` into the existing PR branch without rewriting history.
Keep the activity feed's failed-revoke timestamp cleanup and the approval
service's signer failure logging.

## ETH-214: review round 1 fixes

Demo beats: 2 (request and approve), 5 (revoke).

Paths written:

- `approval/src/lib/service.ts`
- `deploy/env/admin.env.example`
- `docs/ai-usage/core-sol-eth-214.md`

Comment out the five optional environment entries so the system unit keeps its
default PATH. Mask WebSocket URLs in signer diagnostics as well as HTTP URLs.
The existing merge preserves failed-revoke timestamp cleanup before logging.

Verification logs written outside git:

- `/home/anonjr/bramble-work/status/eth-214-review-fixes-verify.log`
- `/home/anonjr/bramble-work/status/eth-214-review-fixes-access-flow.log`
