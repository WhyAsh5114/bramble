# Risks

## Kill criteria

- **K1.** Gate 0.2 fails: two peers on different networks cannot connect without a trusted coordinator, by end of Sunday Sept 6. → Stop and reassess whether to continue this project. Decide Sunday, not day eight. **Resolved Sept 6 — see `10_DAY0_GATES.md` Gate 0.2. Does not apply; project continues. Note the topology caveat there (laptop-to-VPS, not yet the harder two-NAT case).**
- **K2.** EAC delegation does not actually restrict on ENSv2 Sepolia. The ACL story and the ENS pitch both collapse; rethink before continuing. **Resolved Sept 5 — see `10_DAY0_GATES.md` Gate 0.3. Does not apply.**
- **K3.** No physical Ledger device. Ledger slot dies (project survives on two slots).
- **K4.** Direct-connection success rate is so low that nearly everything relays. The cost argument in `01_WHY.md` inverts and the pitch needs rewriting.

## Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| NAT traversal without a coordinator fails or is flaky | Medium (was **High**) | Fatal | Gate 0.2 passed Sept 6 for laptop-to-VPS via relay-only candidate exchange, no hole punching needed. Residual risk is now scoped to the harder two-NAT case, where STUN/hole-punching (still unimplemented) would actually be load-bearing |
| ENSv2 Sepolia instability (beta ~3 weeks old, under audit competition) | Medium | High | Pin working addresses Day 0, do not chase upgrades. Keep a recorded backup of the ENS beats |
| `wallet-cli` v1 experimental, flags may change | Medium | Medium | Pin the version. Cut order step 3–4 removes Ledger cleanly |
| LKRP key rotation destroys prior ciphertext | Medium | Medium | Do not build flows depending on decrypting old data after membership changes. Document it |
| Blocky402 / Hedera friction | Medium | Medium | Gate 0.4 on Day 0, before anything is built on top |
| Scope: three sponsors, two chains, hardest networking problem — **6.5 build days remain: submissions close Sept 13 16:00 UTC / 21:30 IST, not Sept 16** (see `08_EVENT_RULES.md`) | **High** | High | Cut order in `05_BUILD_PLAN.md` — **decide it by Sept 10**, don't discover it Sept 12. A tight two-sponsor submission beats a thin three-sponsor one |
| ETHGlobal check-ins / showcase compliance missed (Sept 8 and Sept 11, 03:59 UTC / 09:29 IST) | Medium | High | Calendar them and track against `08_EVENT_RULES.md`. Missing an ETHGlobal checkpoint risks eligibility independent of code |
| Revocation slower than Tailscale, judge notices | High | Medium | Measure it, publish it, say it out loud in the video |
| Judge says "just run Headscale" | High | High | `06_ADJACENT_WORK.md` line. The answer is that Headscale is the server you are removing, and it lacks Tailnet Lock |
| Judge says "passkeys do this" | High | High | Concede most of it. Fall back to bootstrap, non-equivocation, unwithholdable revocation |
| Overclaiming "zero infrastructure" | Medium | High | Say "no trusted coordinator," never "zero infrastructure" — relays exist and are paid |
| Demo network fails live | Medium | High | Record beats early. Have a recorded fallback for anything network-dependent |

## Honest pros

- Genuinely novel: no blockchain mesh control plane exists.
- The "why a chain" survives scrutiny, which took a long time to get right: bootstrap, non-equivocation, unwithholdable revocation.
- ENS fit is the tightest on the board, and the agent path turns Ledger and Hedera from technically-qualifying into on-theme.
- The author has the pain personally (seat limits on an existing tool), which shows in a demo.
- The relay-sees-only-ciphertext property is a clean, correct reason a permissionless market is safe here.

## Honest cons

- **Implementation risk is the highest of any option considered.** Coordinator-less NAT traversal is harder than the coordinated kind.
- PMF is narrow: teams past the free tier who do not want to run Headscale. Real, not large.
- Enterprises are explicitly out of scope, and that is where the money in this category is.
- Weaker NAT traversal means more relay fallback, which means the cheapest case is also the most fragile.
- Revocation is slower than the incumbent on the metric the headline demo showcases.
- No SSO, audit, or posture checks, which is most of what buyers in this category actually pay for.

## Claims this project does not make

- Not zero infrastructure. Relays exist and are paid.
- Not cheaper than Tailscale at enterprise scale.
- Not NAT traversal parity with Tailscale.
- Not a network effect from more relays.
- Not trustless: peers trust ENSv2 and the Sepolia chain, and enrollment trusts a hardware key holder.
