# AI Usage

This project was built with AI assistance (Claude Code and OpenAI Codex) throughout research, design, review, and implementation. This file points to where that process is recorded, per the event's spec-driven-development transparency requirement — the goal is that a reader can see how the project was directed, not just the resulting code.

- **`docs/00_START_HERE.md` through `docs/11_SOURCE_NOTES.md`** — the working spec, written and iterated on with AI assistance from Day 0 onward, before most product code existed.
- **`docs/adr/`** — individual design decisions, with rationale, made during the build.
- **`docs/11_SOURCE_NOTES.md`** — a research log: partner-workshop findings reconciled against the spec, including contradictions found and how they were resolved, and implementation-time findings (e.g. a docs-vs-deployed-bytecode mismatch caught while validating Gate 0.3).
- **`docs/sources/`** — cleaned transcripts of the two partner workshops used as primary research input.
- **`scripts/gate0.3-eac-check/`** — a validation script (not production code) used to empirically verify a Day-0 feasibility gate against the live hackathon ENSv2 deployment; see its README.
- **`AGENTS.md`** — operating rules for AI agents, including how agents read the Sepolia key (`${SEPOLIA_PRIVATE_KEY:-$SEPOLIA_PK}`) without exposing it.

AI assistance also covered implementation and tests, not only the planning documents. In particular, it was used to draft, revise, and review code in:

- **`brambled/` and `relay/`** — WireGuard admission, service gateway, rendezvous/data relay, payment selection, failover, and their Go tests (for example `brambled/admission/loop.go`, `brambled/gateway/acl.go`, `relay/datarelay.go`).
- **`sidecar/src/` and `relay-sidecar/src/`** — ENSv2 resolution/discovery, x402 Hedera client and resource-server flows, and their TypeScript tests (for example `sidecar/src/ens/client.ts`, `sidecar/src/payments/datarelay.ts`, `relay-sidecar/src/routes/datarelay.ts`).
- **`admincli/src/`** — EAC enrollment and role/ACL commands, Ledger signing and Key Ring integration, and their tests (for example `admincli/src/enroll.ts`, `admincli/src/ledger-eth-sign.ts`, `admincli/src/set-acl.ts`).
- **`admincli/src/lib.ts`, `admincli/src/lib-viem.ts`, `admincli/src/lib.test.ts`** — written by the Core-Claude agent (ETH-28) during the Mumbai build: importable grant/revoke functions extracted from `set-acl.ts`, `revoke-acl.ts` and `revoke.ts`, the viem-backed `Resolver`/`Signer`, and in-memory unit tests. The same change rewrote `admincli/src/set-acl.ts`, `revoke-acl.ts` and `revoke.ts` as thin wrappers and removed `admincli/src/acl-grant.ts`.
- **`dashboard/`** — the read-only application UI and API routes.
- **`agent-mcp/`** — the MCP server that gives a Claude Code agent `request_private_access` and `call_private_service`, with its tests and docs (ETH-32). Written by the Net-Claude agent (Claude Sonnet 5.5), committed as that agent.

- **`rpc-proxy/` and `deploy/rpc-proxy/`** — the W11 memory-only logging JSON-RPC proxy, its tests, and its deployment recipe (`rpc-proxy/src/`, `rpc-proxy/test/server.test.ts`, `deploy/rpc-proxy/README.md`).

- **`deploy/`** — the cloud topology runbook, systemd units, install and check scripts, env-var name templates, and the NAT test setup (`deploy/README.md`, `deploy/REFERENCE.md`, `deploy/TOPOLOGY.md`, `deploy/systemd/`, `deploy/scripts/`, `deploy/nat-lab/`, `deploy/env/`, `deploy/caddy/`, `deploy/stun/`, `deploy/sysctl/`, `deploy/scripts/stun-probe.py`, `deploy/scripts/agent-in-box.sh`) and the ADR `docs/adr/0015-cloud-demo-topology.md`. Written by Infra-Claude under ETH-49 and reviewed by another model family.

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
| Infra-Claude | Demo and infra (took over ETH-30) | `Infra-Claude (agent, Aayush Nair)` | Verified from `GIT_AUTHOR_NAME` on 6 Nov 2026 |
| Hedera-Claude | Demo and infra (took over ETH-71 from Infra-Claude) | `Hedera-Claude (agent, Jay Kerkar)` | Verified from `git var GIT_AUTHOR_IDENT` on 6 Nov 2026 |
| Other lane agents | Network, Tor, Hedera, Demo and infra | to be added by each agent | Open |

### Paths

| Path | Ticket | Agent | Kind |
| --- | --- | --- | --- |
| `README.md` ("Pre-existing vs new", "CROPS" skeleton) | ETH-27 | Core-Claude | Documentation |
| `docs/AI_USAGE.md` (this section) | ETH-27 | Core-Claude | Documentation |
| `brambled/stun/`, `brambled/wgnode/stun.go`, `brambled/statusapi/stun_test.go`, `docs/eth-36-stun-spec.md`, `docs/adr/0017-stun-bind-wrapper-source-selection.md` | ETH-36 | Net-Astra, Net-Claude | Code, tests, documentation |
| `approval/src/lib/chain.ts`, `approval/src/lib/chain-env.ts`, `approval/src/lib/ens-reader.ts`, `approval/src/app/api/[...path]/route.ts`, `approval/test/chain.test.ts`, `approval/test/chain-env.test.ts`, `approval/src/lib/service.ts`, `approval/src/lib/api.ts`, `approval/src/lib/contracts.ts`, `approval/.env.example`, `approval/README.md` (real signer runbook), `approval/next.config.ts`, `approval/tsconfig.json`, `approval/package.json`, `pnpm-lock.yaml`, `admincli/src/lib.ts` (`clearAcl`, exported `deviceName`), `admincli/src/lib.test.ts` | ETH-30 | Infra-Claude | Code, tests, documentation |

## ETH-38: rootless NAT test harness

QA-Sol (OpenAI Codex), committing as `QA-Sol (agent, Aayush Nair)`, writes
`brambled/internal/nattest/nat.go`, `brambled/internal/nattest/nat_test.go`,
`brambled/internal/nattest/PLAN.md`, `brambled/internal/nattest/README.md`,
and `brambled/internal/nattest/VERIFICATION.md`,
and updates `brambled/package.json` and this attribution file.
The human-authored ETH-38 task and Mumbai handoff define the scope and invariants.
The fixture supports W8 demo beat 4; it is not evidence of real NAT traversal.

## Mumbai W10 audit module attribution

- **`hedera-audit/`** — W10 option A, the HCS audit trail module, its tests and its README. Written by the Hedera-Claude agent (Claude), ticket ETH-46.
- **`docs/16_HCS_AUDIT_RECORD_FORMAT.md`** and **`docs/adr/0011-w10-hedera-service-options.md`** — the record-format reference and the draft ADR with three options, for a human to decide. Written by the Hedera-Claude agent (Claude), ticket ETH-46.
- `docs/adr/0011-approval-service-placement.md`, `docs/adr/0012-approval-request-contract.md`, `docs/reference/approval-api.md` — draft ADRs and the approval API reference, written by Hedera-Claude (Claude Sonnet 5.5) for ETH-55, committing as `Hedera-Claude (agent, Jay Kerkar)`. Human approval of the ADRs is pending.

## Network lane: paths written by agents (ETH-37)

Agent: Net-Claude, committing as `Net-Claude (agent, Chaitanya Kelkar)` (Verified from `GIT_AUTHOR_NAME` on 6 Nov 2026). Move these rows into the shared Paths table from ETH-27 when that lands.

| Path                                                | Kind          |
| --------------------------------------------------- | ------------- |
| `brambled/rendezvous/endpoints.go`                  | Code          |
| `brambled/rendezvous/endpoints_test.go`             | Test          |
| `brambled/rendezvous/endpoints_integration_test.go` | Test          |
| `brambled/endpoint_candidate_test.go`               | Test          |
| `brambled/main.go` (`peerEndpointFromCandidate`)    | Code          |
| `docs/reference/endpoint-exchange.md`               | Documentation |

## Mumbai approval API (ETH-29)

Core-Sol (OpenAI Codex), committing as Core-Sol (agent, Yash Kolekar) through
the `whyash-paperclip` commit account,
implemented `approval/` and its API tests from ETH-29. AI assistance also covered
`pnpm-workspace.yaml`, `pnpm-lock.yaml`, `.prettierignore`, `eslint.config.js`,
this file, `approval/README.md`, and the duration draft
`docs/adr/0016-approval-duration.md`. Core-Sol removed its duplicate placement
and request-contract drafts; Hedera-Claude owns ADRs 0011 and 0012 in ETH-55.
Core-Sol integrated Astra's ETH-75 formatting correction without changing the
vendored skill trees.
The task defines the public HTTP test seam and separates real signing into ETH-30.
Human ADR approval and live testnet verification remain required.

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

## Mumbai build: agent-written paths

Baseline: commit `28e2a8f`, tag `pre-mumbai`. Agents wrote the paths below after that commit. Each agent adds a row in the same pull request as the work. List paths, not prose. Humans approve every ADR and merge to `main`.

### Accounts

Each agent commits under the git identity shown here. Confirm yours with `git config user.name` before your first commit.

| Agent | Lane | Commit author name | Status |
| --- | --- | --- | --- |
| Core-Claude | Core and console | `Core-Claude (agent, Jay Kerkar)` | Verified from `GIT_AUTHOR_NAME` on 6 Nov 2026 |
| Core-Sol | Core and console (cross-family reviewer) | to be added by Core-Sol | Open |
| Net-Claude | Network (W8a STUN) | `Net-Claude (agent, Chaitanya Kelkar)` | Verified from commit authorship on `net-claude/eth-36-stun-binding` |
| Keystone | Tor (principal, W9-2 Tor client core) | `Keystone (agent, Yash Kolekar)` | Verified from commit authorship on `keystone/eth-59-tor-client` |
| Tor-Claude | Tor (W9-1 audit, W9-3 ENS and payments; took over ETH-50 from Core-Claude) | `Tor-Claude (agent, Chaitanya Kelkar)` | Verified from commit authorship on `tor-claude/eth-60-tor-ens-pay` and the author of commit `06e52d9` |
| Tor-Astra | Tor (W9-2 recovery) | `Tor-Astra (agent, Yash Kolekar)` | Verified from git author identity on 6 Nov 2026 |
| Tor-Claude | Tor (took over ETH-50 from Core-Claude) | `Tor-Claude (agent, Chaitanya Kelkar)` | Verified from the author of commit `06e52d9` |
| Hedera-Sol | Hedera | `Hedera-Sol (agent, Aayush Nair)` | Verified from `git var GIT_AUTHOR_IDENT` on 6 Nov 2026 |
| Infra-Claude | Demo and infra (took over ETH-30) | `Infra-Claude (agent, Aayush Nair)` | Verified from `GIT_AUTHOR_NAME` on 6 Nov 2026 |
| Other lane agents | Network, Tor, Hedera, Demo and infra | to be added by each agent | Open |

### Paths

| Path | Ticket | Agent | Kind |
| --- | --- | --- | --- |
| `README.md` ("Pre-existing vs new", "CROPS" skeleton) | ETH-27 | Core-Claude | Documentation |
| `docs/AI_USAGE.md` (this section) | ETH-27 | Core-Claude | Documentation |
| `scripts/demo/src/*.ts`, `scripts/demo/package.json`, `scripts/demo/tsconfig.json` | ETH-50 | Core-Claude | Code and unit tests (launcher, preflight, probe, reset) |
| `scripts/demo/README.md` | ETH-50 | Core-Claude | Documentation |
| `scripts/demo/REFERENCE.md` | ETH-50 | Tor-Claude | Documentation (split from the README) |
| `scripts/demo/src/redact.ts`, `scripts/demo/src/run-command.ts`, and the review fixes in `scripts/demo/src/*.ts` and `scripts/demo/README.md` | ETH-50 | Tor-Claude (took over from Core-Claude, 5 Nov 2026) | Code, unit tests, documentation |
| `package.json` (`demo`, `demo:preflight`, `demo:reset` scripts), `pnpm-workspace.yaml`, `pnpm-lock.yaml` | ETH-50 | Core-Claude | Configuration |
| `brambled/stun/`, `brambled/wgnode/stun.go`, `brambled/statusapi/stun_test.go`, `docs/eth-36-stun-spec.md`, `docs/adr/0017-stun-bind-wrapper-source-selection.md` | ETH-36 | Net-Astra, Net-Claude | Code, tests, documentation |
| `approval/src/lib/audit.ts`, `approval/src/lib/audit-runtime.ts`, `approval/src/lib/service.ts`, `approval/src/lib/api.ts`, `approval/src/lib/contracts.ts`, `approval/src/lib/validation.ts`, `approval/src/app/api/[...path]/route.ts`, `approval/next.config.ts`, `approval/tsconfig.json`, `approval/package.json` (HCS commitments) | ETH-48 | Hedera-Claude                                       | Code                                                    |
| `approval/test/audit.test.ts`                                                                                                                                                                                                                                                                                                            | ETH-48 | Hedera-Claude                                       | Test                                                    |
| `approval/src/lib/chain.ts`, `approval/src/lib/contracts.ts`, `approval/src/lib/service.ts`, `approval/src/lib/payments.ts`, `approval/src/lib/validation.ts`, `approval/src/lib/api.ts`, `approval/src/app/api/[...path]/route.ts` (grant digest through the real signer and late confirmation; mirror-node settlement observer), `approval/test/audit.test.ts`, `approval/test/chain.test.ts`, `approval/test/payments.test.ts`, `approval/test/fake-topic.ts` | ETH-48 | Infra-Claude | Code, tests (review fixes from ETH-148) |
| `agent-mcp/tsconfig.json` (`allowImportingTsExtensions`, needed because the access-flow test now loads the approval service with the audit module) | ETH-48 | Infra-Claude | Configuration |
| `approval/README.md` (audit feed, payment report, env), `docs/16_HCS_AUDIT_RECORD_FORMAT.md` (approval integration), `README.md` (W10 row)                                                                                                                                                                                               | ETH-48 | Hedera-Claude                                       | Documentation                                           |
| `approval/src/lib/chain.ts`, `approval/src/lib/chain-env.ts`, `approval/src/lib/ens-reader.ts`, `approval/src/app/api/[...path]/route.ts`, `approval/test/chain.test.ts`, `approval/test/chain-env.test.ts`, `approval/src/lib/service.ts`, `approval/src/lib/api.ts`, `approval/src/lib/contracts.ts`, `approval/.env.example`, `approval/README.md` (real signer runbook), `approval/next.config.ts`, `approval/tsconfig.json`, `approval/package.json`, `pnpm-lock.yaml`, `admincli/src/lib.ts` (`clearAcl`, exported `deviceName`), `admincli/src/lib.test.ts` | ETH-30 | Infra-Claude | Code, tests, documentation |
| `sidecar/src/tor/`, `sidecar/src/routes/tor.ts`, `sidecar/scripts/tor-smoke.ts`, `sidecar/test/tor*.test.ts`, Tor wiring in `sidecar/src/index.ts`, `sidecar/tsconfig.json`, tor-js in `sidecar/package.json` and `pnpm-lock.yaml` | ETH-59 | Keystone | Code, tests |
| `brambled/sidecar/manager.go` (`waitTorReady`), `brambled/sidecar/manager_test.go` | ETH-59 | Keystone | Code, tests |
| `docs/adr/0013-tor-transport.md`, Tor section of `sidecar/README.md` | ETH-59 | Keystone | Documentation |
| `sidecar/src/ens/client.ts`, `sidecar/src/payments/client.ts`, `sidecar/src/payments/datarelay.ts`, `sidecar/test/ens-tor.test.ts`, `sidecar/test/helpers/fake-tor.ts`, Tor wiring in `sidecar/test/device.test.ts` and `sidecar/test/hedera-signer-network.test.ts`, `brambled/admission/tor_startup_test.go`, `brambled/sidecar/client.go` (per-operation budgets), `brambled/sidecar/client_budget_test.go`, `sidecar/src/ens/devices.ts` (required client), `sidecar/src/routes/devices.ts`, `admincli/src/enroll.ts`, `admincli/src/mesh-ip-scan.ts`, `admincli/src/mesh-ip-scan.test.ts`, `admincli/package.json`, `sidecar/src/serve.ts`, `sidecar/src/index.ts` (server timeout), `sidecar/test/serve.test.ts`, `sidecar/test/fixtures/slow-bun-server.ts` | ETH-60 | Tor-Claude | Code, tests |
| `sidecar/src/tor/index.ts`, `sidecar/test/tor.test.ts`, `docs/adr/0013-tor-transport.md`, `docs/AI_USAGE.md` | ETH-59 | Tor-Astra | Shutdown cleanup races, regression tests, documentation |
| `sidecar/scripts/revocation-poll.ts`, the `createEnsClient` seam and optional `client` argument in `sidecar/src/ens/client.ts`, `docs/measurements/2026-11-06-revocation-over-tor.md`, `README.md` ("CROPS", W9 row, revocation sentence), Open items in `docs/adr/0013-tor-transport.md` | ETH-61 | Tor-Claude | Measurement script, documentation |
| `docs/adr/0014-hcs-audit-trail.md`, `docs/spikes/0014-hcs-audit-trail.md`, `hedera-audit/test/freeze-round-trip.ts`, `hedera-audit/test/sdk-freeze.test.ts`, `docs/AI_USAGE.md` | ETH-66 | Hedera-Sol (first draft), Infra-Claude (amendment, test) | Draft design, offline SDK check, documentation |
| `deploy/scripts/software-ring.sh`, `BRAMBLE_RING_CLI` override in `admincli/src/ledger-ring.ts` | ETH-71 | Hedera-Claude | Code (software stand-in for the Key Ring on the cloud admin machine) |
| `admincli/src/enroll.ts` (read-back retry after each write) | ETH-71 | Infra-Claude, Hedera-Claude | Code |
| `brambled/gateway/server.go` (`gatewayDecisionDeadline` 20 s to 90 s) | ETH-71 | Hedera-Claude | Code |

## Mumbai formatting gate correction (ETH-75)

On 6 November 2026 India Standard Time (IST), Astra (OpenAI Codex) updated exactly `.prettierignore` and `docs/AI_USAGE.md` on a branch from `hackathon` commit `3bec957e435283617d44499f13c83eaabf884edc`. The commit account is `Astra (agent, Yash Kolekar) <153986822+whyash-paperclip@users.noreply.github.com>`.

- **Read from source:** Atlas directed the narrow remedy: exclude the vendored `.agents/skills/` and `.claude/skills/` trees from Prettier. Their contents stay unchanged. This restores the baseline gate needed to validate demo beats 1–6.
- **Verified:** Before the correction, Prettier 3.9.6 rejected `code-review/agents/openai.yaml` in both trees. `pnpm install --frozen-lockfile` passed with pnpm 10.33.3.
- **Verified:** `pnpm format:check` passes, and `git diff --exit-code hackathon -- .agents/skills .claude/skills` confirms both skill trees are unchanged.
- **Verified:** With Node.js 24.15.0, Bun 1.4.0 and Go 1.27.3, `pnpm verify` passes formatting, endpoint and local-state checks, typechecking, linting and builds. Its test stage fails in `sidecar/test/devices.test.ts`: the public Sepolia RPC returns code 4444, `pruned history unavailable`, for `eth_getLogs`. The same test fails with the original `.prettierignore`, independently of this correction.
- **Open:** A complete `pnpm verify` pass requires a Sepolia RPC that serves the fixture registry's historical logs. Push and PR creation remain gated on that pass, followed by Core-Claude cross-family approval.

## ETH-29 continuation evidence

Read from source: Atlas permits ETH-29 to push with only the two demonstrated
baseline test failures (ETH-86), compared against `hackathon` at `3bec957`.
No dedicated archive RPC is requested; ETH-86 owns deterministic scan fixtures
and the relay failure investigation. This supersedes the historical endpoint
and push-blocking notes in the ETH-75 evidence above for ETH-29.

## ETH-33 rootless access lifecycle

QA-Sol (OpenAI Codex) wrote `agent-mcp/test/access-flow.test.ts`,
`brambled/gateway/access_flow_test.go`, and `docs/eth-33-rootless-access-flow.md`.
The agent updated `agent-mcp/package.json`, root `package.json`, `pnpm-lock.yaml`,
and this file. The test uses the reviewed ETH-30 approval/admincli adapters and
ETH-32 MCP server; it does not change their production behavior.

Commit identity: `QA-Sol (agent, Aayush Nair) <aayushnair@gmail.com>`.

### ETH-34 — agent payments panel (Core-Sol)

- Agent: Core-Sol; committing as `Core-Sol (agent, Yash Kolekar)
<153986822+whyash-paperclip@users.noreply.github.com>`.
- Task/spec: ETH-34 and `docs/14_MUMBAI_BUILD_BRIEF.md` W3, superseded where
  applicable by `docs/15_MUMBAI_HANDOFF.md`; demo beat 3.
- Prompt: inspect real rendezvous/data-relay purchase timing, reuse the read-only
  mirror feed server-side in approval, show payer/caps/spend/remaining/HashScan,
  verify fixtures and record live verification gaps. Preserve dashboard's boundary.
- Agent-written paths: `approval/src/lib/payments.ts`,
  `approval/src/app/api/payments/route.ts`, `approval/src/app/layout.tsx`,
  `approval/src/app/globals.css`, `approval/src/app/payments/page.tsx`,
  `approval/src/components/payments-panel.tsx`, `approval/test/payments.test.ts`,
  `approval/test/fixtures/payments.json`, `approval/README.md`,
  `docs/16_AGENT_PAYMENTS_FLOW.md`, `docs/17_AGENT_PAYMENTS_REFERENCE.md`,
  `docs/AI_USAGE.md`.
- Human decisions remain the handoff's invariants and H4 approval of the public
  UI. Agent choices: dedicated payer attribution, fixed reporting window,
  advisory budget, separate per-payment cap and fail-closed incomplete totals.

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


## Mumbai payment review corrections (ETH-34)

Core-Sol (OpenAI Codex), using the existing Core-Sol git identity, integrated
`origin/hackathon` (through `1bbca93`) without rewriting history and preserved both attribution
sections in this file. Review corrections cover `approval/src/lib/payments.ts`,
`approval/src/components/payments-panel.tsx`, `approval/test/payments.test.ts`,
`docs/16_AGENT_PAYMENTS_FLOW.md`, `docs/17_AGENT_PAYMENTS_REFERENCE.md`, and
`docs/AI_USAGE.md`. After integration, Core-Sol also added an explicit type for
the patched runtime globals in `sidecar/test/helpers/network-guard.ts`; the
uncached verify run found six TS7017 errors in the incoming audit helper.

- **Verified:** the approval suite passes 21 tests, including successful spend
  alongside duplicate/child records sharing a transaction ID, page overlap,
  both window bounds, multiple payers, duplicate mappings, cap defaults and
  validation, concurrent polls and cache invalidation/expiry.
- **Read from source:** Core-Claude's review identified the consensus-record
  deduplication bug and gaps in the trigger documentation. Core-Sol checked
  `exchangeAnyMetered`, admission endpoint resolution, `watchDataRelayAdopt`,
  the local sidecar route and cap validation before correcting those claims.
- **Open:** funded settlement, deployed cap agreement, public tester screenshots
  and human H4 remain required. These corrections authorize no funded payment or
  deployment and change no handoff invariant.

## Mumbai approval console (ETH-31)

Infra-Sol-UI (OpenAI Codex), committing as `Infra-Sol-UI (agent, Jay Kerkar)`
through the `jaykerkar0405` account, wrote
`approval/src/app/{layout.tsx,page.tsx,globals.css}`,
`approval/src/components/{approval-console,request-card,identity-facts}.tsx`,
`approval/src/hooks/use-requests.ts`, `approval/src/lib/console-api.ts`, the public
identity read in `approval/src/lib/api.ts`, and its API regression test.
AI assistance also covered `approval/e2e/`, `approval/playwright.config.ts`,
`approval/package.json`, `approval/.gitignore`, `pnpm-lock.yaml`,
`approval/README.md`, `docs/eth-31-console-spec.md`,
`docs/how-to-test-approval-console.md`, and this entry.
The console reuses the dashboard shadcn/Radix button in
`approval/src/components/ui/button.tsx`.
The browser harness uses the existing test-only in-memory signer; production
imports no test adapter. UI/HTTP behavior is the task-authorized test seam.
Human H4 screenshot approval and live testnet verification remain required.

Infra-Sol-UI merged `origin/hackathon` at `2403c62` for PR #20 without rewriting
history. Conflict resolution preserves the console styles and all upstream API
and payment changes. The payments shell and selectors are scoped to its route in
`approval/src/app/payments/page.tsx` and `approval/src/app/globals.css`; font
variables remain in `approval/src/app/layout.tsx`. AI assistance also covers
`approval/README.md` and this attribution file.
