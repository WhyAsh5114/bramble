# Infra-Sol-UI: ETH-179 review fixes

Demo beat 2 (onboarding), W11. Continue PR #33 from `a8d499f` in the agent's own worktree.

Paths written:

- `scripts/release/install.sh`
- `scripts/release/uninstall.sh`
- `scripts/release/install.test.sh`
- `docs/reference/install-host.md`
- `docs/ai-usage/infra-sol-ui-eth-179.md`

Verified: regression tests reproduced stale-PID termination, deletion through `$HOME//`, and ambiguous HTTP authority acceptance before the fixes. The updated installer records installation evidence. The uninstaller resolves physical paths and checks process ownership before each signal. `bash scripts/release/install.test.sh` passes 63 checks across sh, dash and BusyBox. `node --test deploy/scripts/gen-node-config.test.mjs` passes all 7 tests. `pnpm verify` passes; package tasks use the shared Turbo cache. The sh, dash and BusyBox matrix covers install, stale PID, and keep-data removal. Native executable discovery, nonpositive PID rejection, protected path aliases, and preservation of an outside script reached through dot segments run under `sh` only.

Open: real binary integration, macOS runtime behavior, and a fresh live-host smoke test. Existing public-host evidence uses a stub bundle. Independent QA-Sol round 2 remains the merge gate; do not merge from this implementation run.
