# AI Usage

This project was built with AI assistance (Claude Code and OpenAI Codex) throughout research, design, review, and implementation. This file points to where that process is recorded, per the event's spec-driven-development transparency requirement — the goal is that a reader can see how the project was directed, not just the resulting code.

- **`docs/00_START_HERE.md` through `docs/11_SOURCE_NOTES.md`** — the working spec, written and iterated on with AI assistance from Day 0 onward, before most product code existed.
- **`docs/adr/`** — individual design decisions, with rationale, made during the build.
- **`docs/11_SOURCE_NOTES.md`** — a research log: partner-workshop findings reconciled against the spec, including contradictions found and how they were resolved, and implementation-time findings (e.g. a docs-vs-deployed-bytecode mismatch caught while validating Gate 0.3).
- **`docs/sources/`** — cleaned transcripts of the two partner workshops used as primary research input.
- **`scripts/gate0.3-eac-check/`** — a validation script (not production code) used to empirically verify a Day-0 feasibility gate against the live hackathon ENSv2 deployment; see its README.

AI assistance also covered implementation and tests, not only the planning documents. In particular, it was used to draft, revise, and review code in:

- **`brambled/` and `relay/`** — WireGuard admission, service gateway, rendezvous/data relay, payment selection, failover, and their Go tests (for example `brambled/admission/loop.go`, `brambled/gateway/acl.go`, `relay/datarelay.go`).
- **`sidecar/src/` and `relay-sidecar/src/`** — ENSv2 resolution/discovery, x402 Hedera client and resource-server flows, and their TypeScript tests (for example `sidecar/src/ens/client.ts`, `sidecar/src/payments/datarelay.ts`, `relay-sidecar/src/routes/datarelay.ts`).
- **`admincli/src/`** — EAC enrollment and role/ACL commands, Ledger signing and Key Ring integration, and their tests (for example `admincli/src/enroll.ts`, `admincli/src/ledger-eth-sign.ts`, `admincli/src/set-acl.ts`).
- **`dashboard/`** — the read-only application UI and API routes.

Mumbai-hackathon agent work (paths written by agents; commits use each agent's own git identity):

- **`docs/W9a_NETWORK_CALL_AUDIT.md`, `sidecar/test/hedera-signer-network.test.ts`, `sidecar/test/helpers/network-guard.ts`** — W9a network-call audit of the Hedera client signer and `brambled` (ETH-40), written by Tor-Claude (Claude Sonnet 5.5).

The team directed the architecture and scope, operated the physical Ledger and live test infrastructure, submitted real testnet transactions, checked resulting chain state, and reviewed generated or AI-assisted changes. The commit history and linked gate/runbook evidence show the progression.

Per-track attribution and involvement narrative for the submission dashboard is written separately by the team, not duplicated here.

## Mumbai build: agent-written paths

- `sidecar/src/tor/`, `sidecar/src/routes/tor.ts`, `sidecar/scripts/tor-smoke.ts`, `sidecar/test/tor*.test.ts`, the Tor wiring in `sidecar/src/index.ts`, `brambled/sidecar/manager.go` (`waitTorReady`) and its tests, `docs/adr/0013-tor-transport.md`: written by the Keystone agent (Claude Opus 5.5, ETH-59), committing as the account in the commit author field.
- `sidecar/src/ens/client.ts`, `sidecar/src/payments/client.ts`, `sidecar/src/payments/datarelay.ts`, `sidecar/test/ens-tor.test.ts`, `sidecar/test/helpers/fake-tor.ts`, the Tor wiring in `sidecar/test/device.test.ts` and `sidecar/test/hedera-signer-network.test.ts`, `brambled/admission/tor_startup_test.go`: ENS reads and x402 payments over Tor (ETH-60), written by Tor-Claude (Claude Sonnet 5.5).

## Mumbai formatting gate correction (ETH-75)

On 6 November 2026 India Standard Time (IST), Astra (OpenAI Codex) updated exactly `.prettierignore` and `docs/AI_USAGE.md` on a branch from `hackathon` commit `3bec957e435283617d44499f13c83eaabf884edc`. The commit account is `Astra (agent, Yash Kolekar) <153986822+whyash-paperclip@users.noreply.github.com>`.

- **Read from source:** Atlas directed the narrow remedy: exclude the vendored `.agents/skills/` and `.claude/skills/` trees from Prettier. Their contents stay unchanged. This restores the baseline gate needed to validate demo beats 1–6.
- **Verified:** Before the correction, Prettier 3.9.6 rejected `code-review/agents/openai.yaml` in both trees. `pnpm install --frozen-lockfile` passed with pnpm 10.33.3.
- **Verified:** `pnpm format:check` passes, and `git diff --exit-code hackathon -- .agents/skills .claude/skills` confirms both skill trees are unchanged.
- **Verified:** With Node.js 24.15.0, Bun 1.4.0 and Go 1.27.3, `pnpm verify` passes formatting, endpoint and local-state checks, typechecking, linting and builds. Its test stage fails in `sidecar/test/devices.test.ts`: the public Sepolia remote procedure call (RPC) endpoint returns code 4444, `pruned history unavailable`, for `eth_getLogs`. The same test fails with the original `.prettierignore`, independently of this correction.
- **Open:** A complete `pnpm verify` pass requires a Sepolia RPC endpoint that serves the fixture registry's historical logs. Push and pull request creation remain gated on that pass, followed by Core-Claude cross-family approval.
