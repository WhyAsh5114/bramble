# Infra-Sol-UI: ETH-244 and ETH-255

Demo beats: 1, 2, 3 and 5; fresh-device enrollment supports beats 2 and 3.

Paths written:

- `scripts/release.sh`
- `scripts/release/install.sh`
- `scripts/release/install.test.sh`
- `scripts/release-smoke.mjs`
- `docs/reference/node-release.md`
- `docs/ai-usage/infra-sol-ui-eth-244.md`

Continues the uncommitted ETH-255 release integration fix from Infra-Claude.
The release bundles the node daemon, its helper and the compiled sidecar.
The installer starts `bramble up` and the daemon embeds the local UI.

Validation recovery on 6 November 2026:

- Updated only the public `APPROVAL_ENS_PARENT` setting in `/home/anonjr/.config/bramble/approval.env` and restarted the existing console service to enable real-tailnet enrollment (ETH-269).
- Wrote validation drivers and evidence under `/home/anonjr/bramble-work/status/eth-244/`: `w2-beats.py`, `w2-install.py`, `w2-join.py`, `sol-w2-*.log`, `sol-w2-*.json`, `sol-w2-public.env`, `sol-w2-public-SHA256SUMS`, `sol-w2-pr.md`, `sol-w2-report.md`, and the isolated `w2-public-device/` runtime.
- Built the ignored `release/bramble_{linux,darwin}_{amd64,arm64}.tar.gz` archives and `release/SHA256SUMS` from committed revision `0368f81`; published them to the existing console distribution host under ETH-244 operator authorization.
- Wrote task evidence and PR #50 work-product metadata through Paperclip.

ETH-270 publisher fix paths:

- `deploy/scripts/gen-node-config.mjs`
- `deploy/scripts/gen-node-config.test.mjs`
- `deploy/env/public.env.example`

Additional recovery paths and checks:

- Updated the public `BRAMBLE_EXTRA_ARGS` line in `/etc/bramble/gateway.env` on the existing gateway to restore automatic discovery. Final value retains its public direct candidate; removed temporary gateway relay-first routing before the metered retry.
- Added `w2-accept.py` and further `sol-w2-*` evidence under the existing status directory.
- ETH-274 extends the same generator/example/test paths with the existing daemon's public metered-rendezvous payment URL.
- Internal review ran twice. The second round accepted the installer TUN correction. The later metered-rendezvous manifest addition has passing generator tests and `pnpm verify`; other-family GitHub review remains pending.

Final evidence and cleanup paths:

- Added `/home/anonjr/bramble-work/status/eth-244/w2-final-test.py` and `w2-cleanup.py` to record the failed relay API test and confirmed revokes.
- Wrote `sol-w2-local-ui-waiting.png`, `sol-w2-local-ui-waiting.json`, `sol-w2-evidence.zip`, and the final status/report evidence in the same status directory. The evidence archive excludes the private device home.
- Stopped the owned test daemon, verified both cloud node services inactive, and handed their slot to ETH-243. Final gateway public flags retain auto-discovery, its direct candidate and sponsored mode.
- Removed only this worktree's generated release tarballs after publication to recover local disk quota. Public distribution archives and recorded checksums remain available.


## ETH-307: disable installer TUN activation

Demo beats: 2 and 3 (installed daemon usability).

Paths written:

- `scripts/release/install.sh`
- `scripts/release/install.test.sh`
- `docs/reference/install-host.md`
- `docs/ai-usage/infra-sol-ui-eth-244.md`

The installer accepts both TUN inputs, warns that TUN is unsupported until the
post-demo ambient-capability fix, and starts the user-space stack without sudo.
The existing startup-argument fixture covers both inputs under sh, dash and
BusyBox, including simulated successful and failed capability grants.

Verified: the regression fixture failed before the fix when the simulated
capability grant succeeded. Validation logs and the isolated default-install
probe are recorded under `/home/anonjr/bramble-work/status/eth-307/` and attached
to the task. No `brambled/` or `sidecar/` source paths changed.
