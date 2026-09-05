# Hedera Workshop — Luke Forrest, Hedera DevRel

**Source:** https://www.youtube.com/watch?v=-nkd3aorELM — "Hedera: Claude Code - AI Skills for Hackathon Builders | Luke Forrest at ETHOnline 2026"
**Method:** cleaned from YouTube's native auto-captions (user-supplied), not local ASR — this transcript is more reliable than the whisper.cpp pass would have been. Cleanup is mechanical (punctuation, corrected mis-transcribed proper nouns, chapter structure kept) — condensed in a few Q&A stretches for readability, not reworded for substance.
**Note:** despite the title, this is not a general Claude Code talk — it is Hedera's own DevRel walking through Hedera-specific tooling (MCP server, skills repo, starter kit, portal, HashScan) using Claude Code as the delivery mechanism. Treat it as a Hedera source, not a generic AI-tooling one.
**Confidence:** speaker is Hedera DevRel — track/prize facts here are primary-source and higher-confidence than a typical attendee talk. Product/URL names are caption-derived and corrected where the phonetic mis-hear was unambiguous; flagged where not.

## Corrections applied

| Heard | Corrected to | Confidence |
|---|---|---|
| Hideera / Hyera / Hydera | Hedera | High |
| clawed / claw (tool name) | Claude | High |
| claw code | Claude Code | High |
| GBT 5.1 | GPT-5.1 | High |
| H bar (as unit) | HBAR | High |
| had error consensus service | Hedera Consensus Service (HCS) | High |
| scaffold har | likely **Scaffold-HBAR** (`npm create scaffold-hbar@latest`), by analogy to Scaffold-ETH and Hedera's HBAR ticker | Medium — not independently confirmed |
| foret | faucet | High |
| portal.padera.com | likely portal.hedera.com | Medium |
| hideera.com/mtcp | likely hedera.com/mcp or a docs subpage for MCP setup | Low |
| "size clause" (Q&A) | likely "besides Claude" (fits the following answer about Codex) | Medium |
| sub 5cality | possibly "sub-5-second finality" (Hedera is known for fast finality) — inferred, not confirmed | Low |
| MPX Skills add | npx skills add | High — matches the exact pattern already in `04_TECH_STACK.md` for Ledger |
| NASA (stray word before "so this is sort of the X402 facilitator") | dropped as a caption artifact | — |

Chat participant names (Rom, Wilfred, Sylvia) left as heard.

---

## Talk, by chapter

**Hedera Docs MCP.** An MCP server exposing Hedera's docs so an AI agent can read and reason over them directly. Setup: on the Hedera docs site, a page has a "Claude Code" setup section — copy the provided command, paste it into Claude, and the MCP server is added with full access to Hedera's documentation.

**Hedera Skills repo.** A companion skills repo, installable as a Claude Code plugin marketplace (`/plugins` → browse → the Hedera marketplace). Five skills, most notably:

- **Hackathon Helper** — a conversational skill, recommended to run before or during building. You feed it the track name/description/requirements/prize details (or paste them in), and it interviews you to build out a PRD for how to integrate Hedera into your idea, asking things like why the design needs to be on-chain, what on-chain properties (e.g. HCS as an immutable record, atomic transactions) actually matter for it. Demoed live building a "bond life cycle" tokenization idea, including a suggestion to use HCS "as an immutable record of events." Confirmed in Q&A: a skill is just a markdown file — it doesn't ship any Claude credits, and can be loaded into any LLM including a local one.
- Also mentioned: an **Agent Kit plugin** in the same skills repo.

**Scaffold-HBAR** (name uncertain, see corrections table). A starter-kit CLI: `npm create scaffold-hbar@latest` (exact package name unconfirmed), prompts for a project name and a template:
- blank
- a cross-chain bridge starter (Axelar / Chainlink / LayerZero)
- **Hedera Native** — no Solidity, no smart contracts; uses Hedera's native services, oracles, on-chain cron/scheduled payments
- **tokenization of subscriptions** — an NFT subscription marketplace starter (e.g. tokenize a gym membership as an NFT, sublet/resell it), positioned as the on-ramp for the tokenization track
- **x402 pay-per-use** — ships with a `facilitator` folder described as "the x402 facilitator... set up for Hedera testnet," plus `agents.md` and `claude.md` files pointing the agent at that spec. Setup also optionally offers to install the Hedera skills marketplace alongside it.

**⚠️ Blocky402 was never named in this talk.** The x402 starter's bundled facilitator is described only as "the x402 facilitator... for Hedera testnet" — Luke does not say Blocky402 anywhere in the session, including during the x402/facilitator walkthrough and the track-prize recap. See reconciliation notes.

**Prize tracks, restated by Hedera DevRel** (primary source, high confidence; see the official ETHGlobal track page for exact amounts):
1. AI and Agentic Payments on Hedera — 3 teams eligible.
2. Open source / improve the "Hedera harness" — 2 teams eligible.
3. Tokenization of anything — 3 teams eligible.
4. Continuity (bringing back an existing Hedera project) — 1 winner. Q&A clarified continuity does **not** require the prior project to have been on Hedera before — any pre-existing project newly integrating Hedera qualifies, by design, to pull in builders who weren't on Hedera previously.

**Portal and HashScan.** `portal.hedera.com` (per corrections table) is where you create a testnet account — recommends **ECDSA** account type over ED25519. Each account gets a daily HBAR allotment from the built-in faucet, capped per person across a handful of accounts, plus a separate top-up faucet if needed — described as "very generous," with an offer to top up further on request. The dev portal also has a contract builder (click-to-compile/validate) and an embedded AI assistant (GPT-5.1-backed, ~32k context — Luke contrasts this with "Claude Code Opus 5" having a much larger context window and suggests using the portal assistant to condense docs before feeding them to your own LLM to save tokens).

**HashScan** — look up an account ID to see its on-chain transaction history (topic ID, payer, fees, etc. for an HCS message submission was the example shown). Luke states directly: **when judging, the Hedera team checks whether a submitted project actually transacts on-chain**, and recommends explicitly calling that out in the submission writeup.

## Q&A highlights

- Smart-contract-capable models besides Claude: Codex mentioned as a personal favorite alongside Claude; open-source options (Qwen) raised by another attendee, no strong recommendation given.
- Scheduled/scheduled-call contract patterns are explicitly fine for the hackathon.
- An existing (non-Hedera) AI-agent project that adds Hedera/x402 integration would likely qualify for the continuity track, per the "should not be specific to Hedera" framing above.
- What scores well in judging, in Luke's own words: projects with a thought-out product lifecycle beyond the hackathon — "not building something... localized to the hackathon," but something meant to reach a live use case / go to market. This is qualitative color, not a rule change.
- Workshop is recorded, will be posted to the hackathon's YouTube.
