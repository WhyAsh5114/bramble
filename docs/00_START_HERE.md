# Start Here

**Working name:** `bramble`. Check npm and GitHub, use it everywhere from the first commit.

**Event:** ETHOnline 2026. The event runs to the Sept 16 finale, but **project submissions (video included) close Sept 13, 16:00 UTC / 21:30 IST** — see `08_EVENT_RULES.md`. ETHGlobal check-ins due Sept 8 and Sept 11 (03:59 UTC / 09:29 IST).
**Track:** Start Fresh.
**Written:** Sept 4, 2026. Re-verify anything version-dependent. (Event schedule corrected Sept 7 against the official event page.)

## The project in one paragraph

A WireGuard mesh network with **no trusted coordination server**. Device authorization lives in an ENSv2 subname registry: each device (or AI agent) is a subname holding its own public key, with Enhanced Access Control governing who may enroll or revoke. Every peer independently resolves that registry and refuses handshakes from keys that are not in it. Relays for NAT fallback are run by anyone and paid per byte via x402 on Hedera, so there are no seats and no subscription. The result is a mesh where no single party can inject a device, and there is no stateful server anyone has to keep alive.

**One line:** your network's guest list lives on-chain, and every door checks it independently.

## Read in this order

| File | What it answers |
|---|---|
| `01_WHY.md` | The problem, who has it, why not the simpler options |
| `02_TRACK_FIT.md` | ENS, Ledger, Hedera — phrase-level mapping |
| `03_ARCHITECTURE.md` | Components, admission model, signaling, what is and is not on-chain |
| `04_TECH_STACK.md` | Verified versions and unknowns |
| `05_BUILD_PLAN.md` | Phases, hard gates, tests, cut order |
| `06_ADJACENT_WORK.md` | Tailscale, Headscale, NetBird, Tailnet Lock — how this differs |
| `07_RISKS.md` | Kill criteria, risk register, honest pros and cons |
| `08_EVENT_RULES.md` | Event compliance: originality, commit history, AI attribution |
| `09_JUDGING.md` | Every objection a judge will raise, with the answer |
| `10_DAY0_GATES.md` | **Do this first.** The checks that decide whether the project is buildable |
| `11_SOURCE_NOTES.md` | Research log: partner workshop findings reconciled against the docs above |
| `adr/` | Individual design decisions, with rationale |

---

## The five things that matter most

1. **Do Day 0 before anything else.** The gates in `10_DAY0_GATES.md` can kill the project. One of them is whether a physical Ledger device is available.
2. **The peer does the checking, not the coordinator.** If admission is verified anywhere central, the whole thesis is theatre.
3. **No endpoints on-chain for mesh members.** Only public keys and authorization. Putting a device's network topology in a public registry is a security regression and a judge will say so.
4. **Do not claim "zero infrastructure."** Relays exist. The claim is "no *trusted* coordinator" and "nothing *you* have to run." Precision here is the difference between credible and dismissed.
5. **Commit continuously.** See `08_EVENT_RULES.md`. Large single commits can disqualify.
