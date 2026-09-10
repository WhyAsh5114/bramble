# Event Rules

## Key dates (verified against ethglobal.com/events/ethonline2026, Sept 7)

| Date (UTC) | Date (IST, UTC+5:30) | What |
|---|---|---|
| Sept 4, 05:00 | Sept 4, 10:30 | Event opened |
| Sept 6, 17:00 | Sept 6, 22:30 | Signup deadline (passed) |
| **Sept 8, 03:59** | **Sept 8, 09:29** | **Project Check-in #1 due** |
| Sept 8, 18:00 / Sept 10, 13:00 | Sept 8, 23:30 / Sept 10, 18:30 | Project Feedback Sessions #1 / #2 (optional, recommended) |
| **Sept 11, 03:59** | **Sept 11, 09:29** | **Project Check-in #2 due** |
| **Sept 13, 16:00** | **Sept 13, 21:30** | **Project submissions due — video included (`requireVideoSubmission` is on). The Sept 16 finale is not the deadline** |
| Sept 13, 19:00 | Sept 14, 00:30 | Judging Round 1: asynchronous project judging (note the IST date rolls to Sept 14) |
| Sept 14, 16:00 | Sept 14, 21:30 | Judging Round 2: live project judging |
| Sept 16, 16:00 | Sept 16, 21:30 | ETHOnline 2026 Finale |

## Start Fresh

All project-specific code written after the event opened (Sept 4). Public libraries and starter kits are fine and should be declared in the README: `wireguard-go`, `pion`, `ensjs`, `@ledgerhq/wallet-cli`, `@x402/*`. Nothing from any prior project is copied in.

## Commit history

Large single commits or missing history may be disqualified, and this is the top risk when building fast with AI assistance. Target **40+ commits across 10+ distinct days**. Commit several times a day at natural stopping points. Never batch.

Check before submitting: `git log --format='%ad' --date=short | sort -u | wc -l` ≥ 10.

Note that 1inch's track explicitly says "no single-commit entries on the final day" — the same expectation applies generally.

## AI tools

Attribution required: document where and how AI tools were used. Spec-driven workflows are permitted **provided all spec files, prompts, and planning artifacts are in the repo.** This doc set ships as `/docs/`. Keep `/docs/adr/` current and add `/docs/AI_USAGE.md`. Submissions relying entirely on AI without meaningful contribution may lose partner prize and finalist eligibility; the ADR trail is the evidence.

## Video

**Required at submission** — the event has `requireVideoSubmission` enabled, so the Sept 13 showcase upload must include it; there is no post-deadline video window. 2–4 minutes for finalist judging; Hedera allows up to 5. 720p minimum. **No TTS or AI voiceover.** Not phone-recorded. Not sped up. Must show the live paid request executing for Hedera. **Record by Sept 12** (build freeze) — see `05_BUILD_PLAN.md`'s calendar.

## Partner prize selection

Up to 3. Multi-track partners count as one. Select **ENS**, **Ledger**, **Hedera**.

## Per-track submission requirements

- **ENS:** built on ENSv2 Sepolia; features central not cosmetic; **functional demo with no hard-coded values**; video or live demo (ideally both); open source.
- **Ledger:** the Key Ring bullets must be built on the Ledger Agent Stack, specifically `wallet-cli ring`.
- **Hedera:** live x402-gated service on Hedera testnet or mainnet settled through **Blocky402**; a platform or agent that consumes it and completes **at least one real paid request end to end**; public repo with README covering setup, architecture, and payment flow; demo video ≤5 minutes showing the paid request executing.

## Team and submission

Team size 1–5. Public repo, open-source license. README with setup, architecture, and run instructions. Showcase page with video and, ideally, a live demo link.
