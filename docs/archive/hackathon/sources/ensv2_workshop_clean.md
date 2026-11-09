# ENSv2 Workshop — Kevin [surname uncertain, ASR: "Werkeman"], ENS Labs

**Source:** https://www.youtube.com/live/CTpZBTFtiow — "ENSv2 - Identity for Apps, Agents & Beyond | Kevin Krone at ETHOnline 2026" (YouTube video title says "Kevin Krone"; the speaker self-identifies only as "Kevin" in audio — treat "Krone" as the likely correct surname over the ASR guess).
**Method:** whisper.cpp, `large-v3-turbo-q5_0`, Metal-accelerated, domain-vocabulary prompt. Cleanup is mechanical (punctuation, capitalization, paragraphing, corrected mis-transcribed proper nouns) — no paraphrasing. Raw ASR output: `transcript_ensv2.txt` / `.srt` (not committed here; regenerate from audio.wav if needed).
**Confidence:** this is ASR, not official captions. Any address, version number, or spelled-out identifier below is **heard, not verified** — cross-check before citing as fact.

## Corrections applied (ASR error → correction)

| Heard | Corrected to | Confidence |
|---|---|---|
| Sipolia / Cipolia | Sepolia | High |
| ENF / ENF v2 | ENS / ENS v2 | High (consistent ASR drop of the S) |
| .eve / .eath / .ev | .eth | High |
| CNS IP25 | ENSIP-25 | High |
| ENF IP26 | ENSIP-26 | High |
| 804 agent / an 804 agent registration | ERC-8004 agent / ERC-8004 agent registration | High |
| UCC | USDC | Medium |
| "DEFL team" | "DevRel team" | Medium |
| "the hacker fund" | possibly "the hackathon" | Low — left as heard, flagged |
| "docs" (bare) | likely docs.ens.domains | Medium, per the earlier raw pass which caught "docs dot [ens].domains" |
| "Firework Factory" (smart contract name) | not corrected — exact contract/factory name unverified | Low — verify against actual ENSv2 docs |

Names of audience members (Javier, Mauricio) are left as heard and unverified.

---

## Talk

Welcome to the ENS workshop with Kevin. Questions in the chat will be addressed at the end.

Kevin: My name is Kevin, I'm a technical writer on the DevRel team at ENS Labs. This is a short, slides-only workshop on the ENSv2 beta deployment on Sepolia. Up front: we deployed another set of contracts to Sepolia specifically for this hackathon, separate from the main beta deployment, and I'd like you to deploy against these. They're linked from a banner on top of the docs site, which points to a feature branch listing the hackathon deployment and how to alter your RPC/endpoint config to talk to these contracts instead of the main ones. I'll post the link in the hackathon's code channel and developer Telegram.

**What ENS is.** ENS turns complex hex addresses into human-readable names — e.g., sending USDC to a name instead of a raw address. Originally a naming/resolution protocol, it grew into an on-chain profile system: avatar, social handles, multi-chain addresses, arbitrary text/data records, all under one name. Forward resolution goes name → address; reverse resolution goes address → name, and when a name's forward resolution points back to the same address that reverse-resolves to it, that pairing is called the **primary name**. Anywhere an app shows a hex address, it should be able to show an ENS name instead — that's the design goal.

**ENSv2, what's new.** ENSv1 used one flat registry keyed by a namehash of the whole tree. ENSv2 makes the tree structure explicit in the contracts: a root registry, then per-name registries nested arbitrarily deep (a name can deploy its own sub-registry, whose owner can deploy further sub-registries, and so on, as long as permissions are granted at each level — confirmed in Q&A). The default posture also changes: in v1 most people used a shared public resolver; in v2 the default is to deploy your **own** resolver, via a proxy factory pattern (one shared implementation, per-name proxies — referred to in audio as "the [Firework?] Factory," exact name unverified). Registries and resolvers are separate concerns — you can deploy a sub-registry (needed if you want to tokenize sub-names or give them different permissions) independently of a resolver, or skip a sub-registry and use wildcard resolution instead if you just want to store data for sub-names without tokenizing them.

A typical setup: a name deploys its own sub-registry plus a **registrar** contract, which governs how sub-names are obtained (e.g. a price per year, or an arbitrary custom rule — own an NFT, whatever). Sub-names can be configured with permissions: whether they can deploy their own resolver or sub-registry, whether they're transferable, and a parent can be configured to drop its own interference rights into the future so sub-names are effectively independent below it.

**EAC — Enhanced Access Control.** New role-based permission system. Roles carry an admin role and can be scoped down to individual capabilities — e.g., permission to change a single text record, or just the avatar, without broader control. Token IDs in ENSv2 are **mutable**: whenever a role is granted or revoked on a name's token, the token is burned and re-minted to the same owner with a **new token ID**. This is deliberate — it stops someone from listing a name with one set of attached permissions and then changing the permission set post-listing to scam a buyer. Simple transfers or renewals do not change the token ID; expiry and pre-registration edge cases do. This is documented in the ENSv2 docs.

**Why v2 over v1/NameWrapper:** more flexible, cleaner design, same lessons-learned motivation. Two aliasing features: (1) record-level aliasing — two names point at the same underlying record set and stay identical when either is edited; (2) registry-level aliasing — two different parent names point at the same sub-registry, so e.g. `a.alice.eth` and `a.bob.eth` can resolve identically if aliased that way.

**AI agents as namespaces.** Kevin's own framing: think of an agent as a sub-name of your name (e.g. `agent.kevin.eth`), with version sub-names underneath it (`v1.agent.kevin.eth`, `v2.agent.kevin.eth`), and the parent `agent.kevin.eth` pointing at whichever version is current.

- **ENSIP-25**: a two-way consent link between an ENS name and an ERC-8004 agent registration. You point at the ERC-8004 registry with an agent ID; if that agent's registration file lists your ENS name as one of its services, and your ENS name in turn points back consenting to that agent, the two-way link is verified.
- **ENSIP-26**: a standardized text record for storing an **agent-to-agent endpoint** on an ENS name. Kevin states this plainly: the standard exists specifically so an agent's reachable endpoint can be published as a text record under the name.

Other build ideas mentioned: wallet UX integration by default, social apps using ENS names as a decentralized username layer, and content-hash-backed fully-onchain websites via IPFS.

**Prizes:** matches `02_TRACK_FIT.md`'s ENS figures exactly (see the official ETHGlobal track page for amounts). Kevin also mentioned a separate **continuity track**, for teams integrating ENSv2 into an app that already existed before the hackathon — this uses the real/production Sepolia deployment, not the hackathon-specific contract set, and isn't currently mentioned anywhere in `02_TRACK_FIT.md`.

## Q&A

- Nested sub-registries under sub-registries: yes, arbitrary depth, gated by permissions granted at each level.
- Fully on-chain website front ends via ENS: possible via content hash, Kevin isn't current on the state of the art, described as "a nice topic to explore."
- Audience member (Javier) building a game: recommended pattern is one name per user as a sub-name of the game's own name (`kevin.javier.eth`-style), using text/data records on those sub-names for in-game state.
- Audience member (Mauricio, Bolivia) building a loyalty/rewards token system: Kevin's suggestion is sub-names per customer wallet instead of raw addresses; on Paymaster/gas-sponsorship, Kevin was explicit that's a different layer of the stack ENS doesn't solve — "ENS is a resolution/discoverability layer," not a payments or gas solution.
