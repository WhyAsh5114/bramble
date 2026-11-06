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

## Mumbai W8a: STUN endpoint discovery

Net-Astra (OpenAI Codex) implemented and tested `brambled/stun/message.go`,
`brambled/stun/bind.go`, `brambled/stun/bind_test.go`, `brambled/wgnode/node.go`,
`brambled/wgnode/stun.go`, `brambled/main.go`, `brambled/statusapi/server.go`, and
`brambled/statusapi/stun_test.go`. Documentation changes cover `README.md`,
`brambled/README.md`, `docs/AI_USAGE.md`, and `docs/eth-36-stun-spec.md`.
Commits use `Net-Astra (agent, Jay Kerkar)` with
`133675904+jaykerkar0405@users.noreply.github.com` and the Paperclip co-author.
The human-authored W8 task and Mumbai handoff set the scope and invariants.

Net-Claude (Anthropic Claude) took over W8a after Net-Astra reached a provider limit. It reviewed and committed the opt-in bind wrapper in `brambled/stun/bind.go`, `brambled/stun/bind_test.go`, `brambled/wgnode/node.go`, `brambled/wgnode/stun.go`, `brambled/main.go`, `brambled/statusapi/stun_test.go`, `brambled/README.md` and `docs/eth-36-stun-spec.md`, and ran the checks. After review (ETH-89) it replaced the shared-endpoint `ClearSrc` call with a fresh endpoint per send and wrote `docs/adr/0017-stun-bind-wrapper-source-selection.md`.
Net-Claude commits as `Net-Claude (agent, Chaitanya Kelkar)` with `chaitanyakelkar07@gmail.com` and the Paperclip co-author.

## Mumbai agent-written paths

Baseline: commit `28e2a8f`, tag `pre-mumbai`. Agents wrote the paths below after that commit. Each agent adds a row in the same pull request as the work. List paths, not prose. Humans approve every ADR and merge to `main`.

### Accounts

Each agent commits under the git identity shown here. Confirm yours with `git config user.name` before your first commit.

| Agent | Lane | Commit author name | Status |
| --- | --- | --- | --- |
| Core-Claude | Core and console | `Core-Claude (agent, Jay Kerkar)` | Verified from `GIT_AUTHOR_NAME` on 6 Nov 2026 |
| Core-Sol | Core and console (cross-family reviewer) | to be added by Core-Sol | Open |
| Net-Claude | Network (W8a STUN) | `Net-Claude (agent, Chaitanya Kelkar)` | Verified from commit authorship on `net-claude/eth-36-stun-binding` |
| Keystone | Tor (principal, W9-2 Tor client core) | `Keystone (agent, Yash Kolekar)` | Verified from commit authorship on `keystone/eth-59-tor-client` |
| Tor-Claude | Tor (W9-1 audit, W9-3 ENS and payments) | `Tor-Claude (agent, Chaitanya Kelkar)` | Verified from commit authorship on `tor-claude/eth-60-tor-ens-pay` |
| Tor-Astra | Tor (W9-2 recovery) | `Tor-Astra (agent, Yash Kolekar)` | Verified from git author identity on 6 Nov 2026 |
| Other lane agents | Network, Tor, Hedera, Demo and infra | to be added by each agent | Open |

### Paths

| Path | Ticket | Agent | Kind |
| --- | --- | --- | --- |
| `README.md` ("Pre-existing vs new", "CROPS" skeleton) | ETH-27 | Core-Claude | Documentation |
| `docs/AI_USAGE.md` (this section) | ETH-27 | Core-Claude | Documentation |
| `brambled/stun/`, `brambled/wgnode/stun.go`, `brambled/statusapi/stun_test.go`, `docs/eth-36-stun-spec.md`, `docs/adr/0017-stun-bind-wrapper-source-selection.md` | ETH-36 | Net-Astra, Net-Claude | Code, tests, documentation |
| `sidecar/src/tor/`, `sidecar/src/routes/tor.ts`, `sidecar/scripts/tor-smoke.ts`, `sidecar/test/tor*.test.ts`, Tor wiring in `sidecar/src/index.ts`, `sidecar/tsconfig.json`, tor-js in `sidecar/package.json` and `pnpm-lock.yaml` | ETH-59 | Keystone | Code, tests |
| `brambled/sidecar/manager.go` (`waitTorReady`), `brambled/sidecar/manager_test.go` | ETH-59 | Keystone | Code, tests |
| `docs/adr/0013-tor-transport.md`, Tor section of `sidecar/README.md` | ETH-59 | Keystone | Documentation |
| `sidecar/src/ens/client.ts`, `sidecar/src/payments/client.ts`, `sidecar/src/payments/datarelay.ts`, `sidecar/test/ens-tor.test.ts`, `sidecar/test/helpers/fake-tor.ts`, Tor wiring in `sidecar/test/device.test.ts` and `sidecar/test/hedera-signer-network.test.ts`, `brambled/admission/tor_startup_test.go`, `brambled/sidecar/client.go` (per-operation budgets), `brambled/sidecar/client_budget_test.go`, `sidecar/src/ens/devices.ts` (required client), `sidecar/src/routes/devices.ts`, `admincli/src/enroll.ts`, `admincli/src/mesh-ip-scan.ts`, `admincli/src/mesh-ip-scan.test.ts`, `admincli/package.json` | ETH-60 | Tor-Claude | Code, tests |
| `sidecar/src/tor/index.ts`, `sidecar/test/tor.test.ts`, `docs/adr/0013-tor-transport.md`, `docs/AI_USAGE.md` | ETH-59 | Tor-Astra | Shutdown cleanup races, regression tests, documentation |

## Mumbai formatting gate correction (ETH-75)

On 6 November 2026 India Standard Time (IST), Astra (OpenAI Codex) updated exactly `.prettierignore` and `docs/AI_USAGE.md` on a branch from `hackathon` commit `3bec957e435283617d44499f13c83eaabf884edc`. The commit account is `Astra (agent, Yash Kolekar) <153986822+whyash-paperclip@users.noreply.github.com>`.

- **Read from source:** Atlas directed the narrow remedy: exclude the vendored `.agents/skills/` and `.claude/skills/` trees from Prettier. Their contents stay unchanged. This restores the baseline gate needed to validate demo beats 1–6.
- **Verified:** Before the correction, Prettier 3.9.6 rejected `code-review/agents/openai.yaml` in both trees. `pnpm install --frozen-lockfile` passed with pnpm 10.33.3.
- **Verified:** `pnpm format:check` passes, and `git diff --exit-code hackathon -- .agents/skills .claude/skills` confirms both skill trees are unchanged.
- **Verified:** With Node.js 24.15.0, Bun 1.4.0 and Go 1.27.3, `pnpm verify` passes formatting, endpoint and local-state checks, typechecking, linting and builds. Its test stage fails in `sidecar/test/devices.test.ts`: the public Sepolia remote procedure call (RPC) endpoint returns code 4444, `pruned history unavailable`, for `eth_getLogs`. The same test fails with the original `.prettierignore`, independently of this correction.
- **Open:** A complete `pnpm verify` pass requires a Sepolia RPC endpoint that serves the fixture registry's historical logs. Push and pull request creation remain gated on that pass, followed by Core-Claude cross-family approval.

## Hermetic verification after pruned RPC history and relay failover flake (ETH-86)

On 6 November 2026 IST, Keystone (Claude Code, Claude Opus 5.5) wrote the change on branch `keystone/eth-86-verify-gate`, stacked on the ETH-75 branch `astra/eth-75-format-gate` (`84bce16`), which is based on `hackathon` `3bec957`. Paths written:

- `brambled/internal/testport/testport.go` (new)
- `brambled/rendezvous/client.go`
- `brambled/rendezvous/client_test.go`
- `brambled/datarelay_endpoint_test.go`
- `brambled/admission/gate1_3_test.go`
- `brambled/gateway/forward_test.go`
- `brambled/gateway/server_test.go`
- `brambled/gateway/realservice_manual_test.go`
- `brambled/rendezvous/gate1_2_test.go`
- `brambled/rendezvous/integration_test.go`
- `brambled/wgnode/node_test.go`
- `sidecar/src/ens/devices.ts`
- `sidecar/test/devices.test.ts`
- `sidecar/test/fixtures/registry-logs.ts` (new)
- `docs/adr/0010-full-mesh-peer-discovery.md`
- `docs/AI_USAGE.md`

Findings and changes:

- **Verified:** Public Sepolia (`ethereum-sepolia-rpc.publicnode.com`) answers `eth_getLogs` over the tailnet registry's history with error 4444, `pruned history unavailable`. A human decided the project uses no dedicated archive RPC. `sidecar/test/devices.test.ts` now injects a fixture-backed RPC client, built with viem's encoders from the exact `LabelRegistered` ABI, into `listDeviceLabels`. The test asserts the exact deduplicated label list, contiguous `eth_getLogs` windows of at most 9,000 blocks, the registry address and event topic, and that a 4444 error rejects. The live scan still exists and runs only with `BRAMBLE_LIVE_SEPOLIA=1`.
- **Verified:** The fixture tests fail when the logic breaks. Four deliberate mutations each made at least one test fail: removing deduplication, widening the window to 10,000 blocks, stopping the page loop one window early, and scanning the wrong registry address.
- **Read from source:** `listDeviceLabels` feeds mesh-ip allocation in `admincli/src/enroll.ts`. It keeps failing hard on 4444 rather than skipping with a warning, because a partial label list could hand out a mesh-ip another device already holds.
- **Verified:** Brambled tests bound fixed UDP ports such as 61960. Other agents' `go test` processes on the same host held those ports, and the failover test failed with `bind: address already in use`. WireGuard nodes in tests now use `ListenPort: 0` where every endpoint is a relay address, and `testport.FreeUDP` elsewhere. Two full `go test ./...` runs in parallel no longer collide.
- **Verified:** `TestDataRelayFailover_SwitchesToBackupRelayWhenPrimaryStalls` also failed about 1 run in 5 with free ports, after about 25.85 seconds, with `alice and bob failed to reconnect through the backup relay after failover`. A traced failing run showed two causes:
  1. The test's fake rendezvous relay never deregistered closed connections, unlike `relay/main.go`. It forwarded and acked alice's new candidate to a connection bob had just closed. The fake now deregisters on disconnect.
  2. `rendezvous.Exchange` resent its own offer only on a one-second tick. When the peer's offer arrived in the last second before the deadline, the call timed out and dropped the peer's candidate, which the relay had already acked. `Exchange` now resends at once when the peer's offer arrives. `TestExchangeResendsOfferAsSoonAsPeerArrives` failed 3 of 3 runs before the fix and passes after it.
- **Verified:** With both changes, the failover test passed 20 of 20 runs, in 4 parallel processes. With only the fake relay fixed, it failed 4 of 15.
- **Open:** `Exchange` still loses the peer's candidate if the peer's offer arrives within one relay round trip of the deadline. The window is now one round trip instead of up to one second.
- **Open:** `sidecar/test/device.test.ts` still reads current Sepolia state live. Current state is not pruned, so it passes today, but it is not hermetic.
