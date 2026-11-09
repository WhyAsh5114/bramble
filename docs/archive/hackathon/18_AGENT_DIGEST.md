# Agent digest

One-page summary of `docs/15_MUMBAI_HANDOFF.md`. Read this first. Open the full handoff or `docs/14_MUMBAI_BUILD_BRIEF.md` only for a section your ticket names, and read just that section. The handoff wins on conflicts.

Deadline: Saturday 7 November 2026, 09:00 IST. Baseline commit: `28e2a8f` (tag `pre-mumbai`). Everything after it is new work.

## Goal

Enter Bramble on the continuity track. The prizes judge new work only:

- Hedera continuity: one new Hedera service (W10).
- EF CROPS upgrade: the control plane runs over Tor (W9).
- Finalist: a live demo of the access lifecycle.

## Required work

| ID | What | Done when |
| --- | --- | --- |
| W1–W5 | Approval console, MCP agent, payments in the flow, activity feed, launcher | Beats 1–3 and 5 run on the cloud nodes |
| W8 | STUN discovery and hole punching, with automatic relay fallback | Two nodes behind separate NATs form a direct WireGuard path with no manual endpoint flag; a blocked path falls back to the relay |
| W9 | Sidecar ENS reads and x402 payments go through Tor (`torFetch('ens')`, `torFetch('pay')`) | The logging proxy shows only Tor exit IPs; revocation time over Tor is measured and in the README |
| W10 | Option A: salted-hash commitments posted to a Hedera HCS topic | A HashScan link appears in the console |
| W11 | Cloud nodes, logging proxy, public console, `pnpm demo`, reset, preflight | Two clean full runs after a reset |

Never cut W8, W9, W10 or beats 1–5. Ledger signing (W7) is out of scope.

## Invariants

1. No central enforcement. Peers check access themselves from ENS.
2. No topology on-chain (`pnpm check:no-endpoints`).
3. No local-state writes in `brambled` or `sidecar/src` (`pnpm check:no-local-state`).
4. `dashboard/` stays read-only.
5. Access records are digests, never plaintext names, hosts or ports.
6. The agent never holds a backend credential.
7. `pnpm verify` passes before every push.
8. Claims are honest: "no trusted coordinator", not "no infrastructure".

## Demo beats

1 Denied. 2 Request and approve. 3 Works and pays (HashScan link and the new Hedera record). 4 Direct path (STUN, hole punch, relay fallback). 5 Revoke (the WOW moment). 6 What the provider sees (before and after Tor). Every ticket traces to a beat.

## Event rules

- Small, frequent conventional commits. A single large commit risks disqualification.
- Keep the README "Pre-existing vs new" section current.
- Record your paths in your own `docs/ai-usage/<your-agent-name>.md` (see `AGENTS.md`).
- Never commit or print keys or authenticated RPC URLs.
- Only a human merges to `main`.
