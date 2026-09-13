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

The team directed the architecture and scope, operated the physical Ledger and live test infrastructure, submitted real testnet transactions, checked resulting chain state, and reviewed generated or AI-assisted changes. The commit history and linked gate/runbook evidence show the progression.

Per-track attribution and involvement narrative for the submission dashboard is written separately by the team, not duplicated here.
