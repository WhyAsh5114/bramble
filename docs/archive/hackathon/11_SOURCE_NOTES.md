# Source Notes — Day 1 Partner Workshops

Reconciliation of ETHOnline 2026 partner workshop content against the docs in this folder. Sources are transcripts, not primary docs — cleaned copies live in `docs/sources/`. Nothing below has been written back into `01`–`11` silently; deltas are folded in deliberately, with the applied change noted.

**Status:** ENS (Kevin, ENS Labs) and Hedera (Luke Forrest, Hedera DevRel) covered. **Ledger's "tracks explained" video hasn't aired yet** — this file is written to be appended to, not complete. Add a `## Ledger` section when that lands.

**Confidence key:** 🔴 act on this — contradicts or materially changes an existing doc. 🟡 new info, not yet in docs, worth a look. 🟢 corroborates what's already written, no action needed. ⚪ heard on tape, unverified — do not promote to "verified" status anywhere.

---

## ✅ RESOLVED — ENSIP-26 stores agent endpoints, but the fix is "public vs. private entity," not "never populate it"

Original finding: `03_ARCHITECTURE.md`'s hard rule said "nothing that reveals network topology goes on-chain," and ENSIP-26 — which `02_TRACK_FIT.md`/`04_TECH_STACK.md` recommend adopting — is, per Kevin's description, a standard text record specifically for an **agent-to-agent endpoint**. First pass here read that as a straight conflict and recommended never populating the field.

That was too blunt. The real distinction is **who the entity is, not whether ENSIP-26 exists**:

- **Device/agent (mesh-member) subnames** — admission control gates these; the entire security property is that nobody outside the authorized set can find where they live. Never populate an endpoint here.
- **Relay subnames** — already a permissionless, publicly-advertised commodity service in `03_ARCHITECTURE.md`'s own design ("any operator runs a relay and advertises it," and Hedera awards points for agent-discoverable relay directories). Publishing a relay's endpoint via ENSIP-26 isn't a leak, it's the directory feature the Hedera track is asking for.

**Applied:** `03_ARCHITECTURE.md`'s hard rule now states the public/private split explicitly; Gate 1.4 in `05_BUILD_PLAN.md` now scopes the no-endpoints rule to device/agent sub-registries specifically (and requires relay and device/agent records to live in structurally separate sub-registries, not just be runtime-checked apart); `09_JUDGING.md` objection 13 answers "why doesn't your agent use ENSIP-26" directly.

---

## ✅ RESOLVED — Hackathon-specific ENSv2 Sepolia deployment, addresses confirmed

Originally flagged from Kevin's workshop as "there's a banner, find it." Since confirmed directly: **Kevin | ENS posted the full deployment in the hackathon Discord on Sept 4**, which is a stronger source than the transcript (official, written, includes exact URLs and a gotcha the spoken talk didn't mention).

- Deployment addresses + viem/ethers snippets: https://feature-permres-inode-refact.docs-bao.pages.dev/learn/deployments#sepolia-ensv2-beta
- ENSv2 docs for this deployment: https://feature-permres-inode-refact.docs-bao.pages.dev/ensv2/overview
- ENS Explorer (hackathon deployment only): https://hackathon-deployment-portal-app.ens-cf.workers.dev/
- ENS App, register hackathon names here: https://hackathon-deployment-manager-app-v4.ens-cf.workers.dev/
- **New from Discord, not in the talk:** viem/ethers ship a built-in Universal Resolver address that must be overwritten with the hackathon's Universal Resolver, or resolution silently targets production instead.

**Applied:** all of this is now in `04_TECH_STACK.md` (new "Hackathon ENSv2 Sepolia deployment" section) and `10_DAY0_GATES.md` Gate 0.3 (now step one of the gate, before registering anything).

---

## Why ENSv2 token ID mutability matters — not framing, a specific implementation trap

Asked directly: does token mutability affect the project's fit, or is it just color? **It doesn't threaten the thesis or the ENS fit — the registry's current authorization state is still what peers check, regardless of the token's ID.** But it's a real trap in exactly the two gates that *are* the project:

- Mutability triggers on **EAC role grant/revoke specifically** — not on transfers, renewals, or arbitrary resolver writes (e.g. rotating a device's pubkey text record doesn't touch the token). So it's narrower than "everything churns," but it lands precisely on the operations Gates 1.3 and 2.1 exercise.
- A burn + re-mint emits an ERC-721 Transfer event indistinguishable in shape from a genuine ownership transfer. Any code that watches Transfer events keyed by token ID to infer "did this device change hands" will misfire on a plain role change — the fix is to key off the **name** (or owner address), not the token ID, and to re-read current role state rather than diffing against a token ID captured earlier.
- Concretely: Gate 1.3's revocation-detection logic and Gate 2.1's table-driven role test both need to re-query current token ID / current roles after each operation rather than asserting against one captured at test start. Caution notes now added to both gates in `05_BUILD_PLAN.md`. Any future demo UI that displays a device "by token ID" should display by name instead — the ID isn't stable across the demo's own revoke/rotate beats.

---

## 🔴 Token IDs are mutable on role grant/revoke — Phase 2 needs to account for this

Not mentioned anywhere in the current docs. Kevin: in ENSv2, whenever a role is granted or revoked on a name's token (EAC permission change), **the token is burned and re-minted to the same owner with a new token ID** — deliberately, so a seller can't sell a name with one permission set then quietly change it post-sale. Plain transfers and renewals don't trigger this; expiry and pre-registration do.

This is directly relevant to `03_ARCHITECTURE.md`'s admission model and `05_BUILD_PLAN.md` Gate 2.1 (EAC role tests) and Gate 1.3 (revocation propagation). If revoking a device's enrollment role is itself an EAC role change on that device's subname token, revocation will look like a burn + new mint at the ERC-721 level, not a simple state flip. Any code that tracks devices by token ID (event indexing, caching, "has this device's token ID changed since I last saw it" logic) needs to treat token ID as **unstable across permission changes** and key off the subname/owner instead.

**Action:** re-read Gate 1.3's revocation test and Gate 2.1's EAC table-driven test with this in mind — confirm neither assumes token ID stability.

---

## ✅ CONFIRMED — Blocky402 is required, per the official track page

Flagged from the Hedera DevRel talk never naming Blocky402 while walking through Hedera's own x402 tooling. Checked directly against the official ETHGlobal ETHOnline 2026 prizes page (primary source, higher authority than either workshop): the "AI & Agentic Payments on Hedera" track's qualification requirements state verbatim: *"Host a live x402-gated service on Hedera testnet or mainnet, settled through the Blocky402 facilitator."* Confirmed, not a docs error.

**Still open, narrower than before:** if you use Scaffold-HBAR's x402 starter kit, its bundled `facilitator` folder was described in the workshop only as "the x402 facilitator... set up for Hedera testnet" — Luke never confirmed it *is* Blocky402. Check that before Gate 0.4; if it's a different/generic facilitator, swap in Blocky402 explicitly rather than assuming the starter satisfies the requirement out of the box.

---

## 🟢 Corroborations — no action, just confirms existing docs

- **EAC scoping to a single field.** Kevin: EAC can grant permission to "change like a single text record or like change the avatar for a name" — matches `02_TRACK_FIT.md`'s exact framing ("letting an account edit only certain text records") almost verbatim.
- **ERC-8004 agent identity.** Kevin's description of ENSIP-25 (two-way consent link between an ENS name and an ERC-8004 agent registration) confirms and sharpens `02_TRACK_FIT.md`'s "extra points reachable: ERC-8004 agent identity" line — concrete mechanism now understood: point at the registry with an agent ID, check the agent's registration lists your ENS name, and your name points back consenting.
- **HashScan / on-chain proof requirement.** Luke stated directly that Hedera judges check whether a submission actually transacts on-chain, and recommends calling it out explicitly in the writeup — reinforces `05_BUILD_PLAN.md` Gate 4.1's "prints a transaction ID viewable on HashScan" requirement. Worth literally following Luke's advice: state in the README that the demo's HashScan transaction is intentional evidence, not incidental.
- **`npx skills add` pattern.** Luke describes the same install pattern for Hedera's skills repo that `04_TECH_STACK.md` already documents for Ledger's (`npx skills add LedgerHQ/agent-skills -s wallet-cli-usage`). Not required for bramble's scope, but if the team wants Hedera-doc-aware tooling while building Phase 4, it's a real option — add the Hedera Docs MCP server (`docs.hedera.com`, exact setup command not confirmed here — see `docs/sources/hedera_workshop_clean.md`) the same way.

## 🟡 New info, low relevance to bramble, no action needed

- **ENS continuity track** (requires integrating ENSv2 into a pre-existing app against the *production* Sepolia deployment, not the hackathon one) — not in `02_TRACK_FIT.md` currently, but bramble is declared Start Fresh (`00_START_HERE.md`), so this track is inapplicable. Only relevant if partner-prize selection UI asks about it.
- **Hedera continuity, open-source, and tokenization tracks** — real prize categories, but not the one bramble is targeting (AI & Agentic Payments). No action.
- **ENSv2 aliasing (record-level and registry-level)** — a real feature, no obvious use in a device-registry mesh VPN. Noted for completeness.
- **Hedera testnet faucet mechanics** (1,000 HBAR/24h per account, up to 5 accounts, extra 100 HBAR faucet) — useful operational detail for Gate 0.4, not architecture-changing.

## ⚪ Heard, not verified — do not cite as fact without checking

- Exact ENSv2 proxy/factory contract name (heard as "Firework Factory" — almost certainly not the real name).
- Hedera Scaffold kit's actual package name (guessed as `scaffold-hbar`) and the Hedera MCP setup URL/command — see the corrections tables in the two `docs/sources/*.md` files for the full list of phonetic ASR/caption guesses.
- "Sub-5-second finality" as a reason to put data on Hedera — inferred from a garbled caption fragment, not something Luke or Kevin actually said outright.

---

## 🔴 Docs vs. deployed bytecode drift — UserRegistry proxy deployment

Found while running the Gate 0.3 K2 check (above), not from a transcript. The ENSv2 docs' "Deploying a Registry Proxy" example gives `UserRegistryImpl.initialize(address rootAccount, uint256 roleBitmap)` as the initializer to encode for `VerifiableFactory.deployProxy`. On the hackathon deployment, that reverts every time — confirmed via `eth_call` simulation, not just a failed transaction.

Ground truth (verified source on Sepolia Etherscan, contract `UserRegistry` at `0x47b442d0cf617c41cabaff5f02f44dd1e5f72546`): the real initializer is

```
initialize((address account, uint256 roleBitmap)[] grants)
```

a single `Grant[]` array — the same shape as the Permissioned Resolver's `grants` argument, just without the resolver's separate `calls` array. Confirmed empirically: deploying with empty `data` succeeds (proves the deploy mechanism itself is fine), the two-arg docs version reverts inside the nested init call every time, and the corrected one-array version succeeds and produces a working, registerable subregistry.

**Action:** anyone implementing Phase 2's subname registry deployment from the docs directly will hit this. Use the corrected signature. Working reference implementation: `scripts/gate0.3-eac-check/index.mjs`, function `deployUserRegistryProxy`.

**Also worth knowing:** resolver and registry proxy addresses are fully deterministic (CREATE2 keyed on `owner + version`, or `owner + namehash + version` for registries) — redeploying with the same version number a second time reverts with a plain address-collision failure, not a helpful error. Vary the version per deployment (or treat "already deployed" as success and just read the predicted address) rather than hardcoding `0n`.

## Day 1 technical sanity check — feasibility only, not effort/timeline

Ranked by what a failure invalidates, not by when it's due.

### Run today — binary, external, can kill a track

1. **✅ DONE, PASSED (Sept 5).** EAC restriction, verified end-to-end against the live hackathon deployment, not simulated. Full writeup and script in `10_DAY0_GATES.md` Gate 0.3. K2 does not apply.
2. **Design confirmed by primary source (Ledger's own Sept 7 workshop); still needs the actual physical-device run.** `wallet-cli ring decrypt` on a host with the Ledger device absent is Gate 0.1's sub-check, already correctly called "the entire premise of the Ledger enrollment story." Ledger's own speakers confirmed this is the *intended, designed-for* use case (VPS/CI-runner enrollment, named explicitly), not a hopeful assumption — see the "✅ RESOLVED" entry below. What's still pending is running it for real once the device is physically on hand: `ring init`/`ring encrypt` with the device present, `ring decrypt` on a second machine without it. **✅ RESOLVED (Sept 8) — whether `ring` works under Speculos:** it doesn't, and not for the reason first guessed. `wallet-cli` is hardware-only by its own README; the only public Speculos-only Ledger CLI, `live-cli`, has no `ring`/Key Ring command in its fixed e2e command set at all. No publicly available tool exposes Key Ring over Speculos. Full citation and the one open lever (asking Ledger's Discord directly about a staging-trustchain path) in `adr/0001-ledger-ring-vs-send-split.md`.
3. **Also unverified, cheap to check without hardware:** does `wallet-cli send` accept arbitrary contract calldata, or only native-asset transfers? `02_TRACK_FIT.md` and `05_BUILD_PLAN.md` Gate 3.1 depend on `send` being able to sign a `grantRoles`/`revokeRoles` call on the ENS registry, not just move a native asset. Check `wallet-cli send --help` for a `--data`/calldata flag before assuming this path works as designed.

### Design gaps — resolvable on paper, real but not fatal

4. **Gate 1.3 conflates two different revocation numbers.** WireGuard has no teardown/revocation message. An already-established tunnel with live session keys keeps passing traffic until either the local peer entry is actively removed from the wireguard-go config, or WireGuard's own rekey timer fires (~2 min by default). If the admission verifier only gates *new* handshakes, "time to connection drop" will read fast for a peer that hasn't connected yet, while an *already-connected* revoked peer stays up for up to ~2 minutes regardless — a materially different, and more attack-relevant, number. **Fix:** the registry-refresh loop must actively remove revoked peers from the live wireguard-go peer table on every poll, not just refuse future handshakes. Gate 1.3's test should say explicitly which of the two scenarios it measures (and ideally measure both). Now reflected directly in `05_BUILD_PLAN.md` Gate 1.3.
5. **✅ RESOLVED — Signaling rendezvous discovery isn't specified for the multi-relay case.** Distinct from the hole-punch-success risk `07_RISKS.md` already tracks: before any connection exists, peer A has no way to know *which* relay instance to send peer B's offer through, if relay choice is arbitrary and per-connection (chosen by price/latency, per `03_ARCHITECTURE.md`). A single hand-run relay hides this for Gate 0.2's Day-0 test; it surfaces at Gate 4.3 ("two relays, client chooses"). **Design, recorded in `adr/0003-rendezvous-relay-split.md`:** split roles — a small set of **rendezvous relays per tailnet, published via ENS**, that every member keeps light presence on purely for signaling, separate from the **data-plane relay**, which stays the permissionless, price/latency-chosen market already designed. Preserves the "no fixed DERP-style set" claim in `06_ADJACENT_WORK.md` while solving discovery. Applied to `03_ARCHITECTURE.md` and `05_BUILD_PLAN.md` Phase 4.
6. **x402 must gate a session/bandwidth-allotment purchase, not individual packets.** Gate 4.2 ("cost scales with bytes") could naively be built as a literal per-packet HTTP 402 challenge, which is incompatible with real-time UDP tunneling. The Hedera track's own "settle every few seconds" framing already implies periodic settlement of accumulated usage — now an explicit design constraint in `05_BUILD_PLAN.md` Gate 4.2.

### ✅ RESOLVED — Solving item 5 exposed a second problem: data-relay payment volume could be near zero

Asked directly, after the rendezvous/data split above: if data relays are only used when hole punching *fails*, and a lot of realistic topologies succeed at hole punching (a laptop connecting to a VPS needs no traversal at all on the VPS's side, and will likely just connect), doesn't that mean the paid relay path — the thing Hedera's track actually wants to see settled, repeatedly, on camera — might rarely or never fire? Yes, and nothing in the original design hedged against it. This cuts the *opposite* direction from `07_RISKS.md` K4 (which worries about *too much* relaying breaking the cost story) — nobody had asked about too little.

**Fix, folded into `adr/0003-rendezvous-relay-split.md`:** meter the rendezvous relay too, even though its payload is tiny. Every connection attempt — hole-punch success or not — now pays a small rendezvous fee, guaranteeing real, repeated, on-chain metered activity regardless of how often the data relay is actually needed. The data relay's price/latency-scaled fee is still demonstrated (Gate 4.3 requires it structurally), but it's no longer the *only* evidence of a working paid service. Applied to `03_ARCHITECTURE.md` ("Relay economics"), `05_BUILD_PLAN.md` Gate 4.1/4.2 (now requires at least one demoed connection that uses a topology which cannot hole-punch, so the data-relay path is shown live, not just asserted in a test).

### ✅ RESOLVED — Does the rendezvous relay quietly reintroduce a trusted coordinator?

Asked directly, after the rendezvous/data split above: peers still check ENS directly for authorization, so what happens if a rendezvous relay goes rogue — does it just refuse to let peers connect, with nothing else falling through? Yes, and it's worth stating precisely why, because it's exactly the question a networking-literate judge will ask.

A rogue rendezvous relay can withhold or lie about a candidate address, but it cannot make an impersonation succeed: a peer's WireGuard handshake is addressed to a public key already obtained independently from ENS, and Noise_IK only completes if the responder holds the matching *private* key. A substituted address just produces a failed handshake, never a compromised one — the relay controls *where* a peer knocks, never *who* is allowed to answer. That's an availability failure class, not an admission-integrity one, and it's categorically different from a compromised Tailscale/Headscale coordinator, which *can* inject a fully-legitimate-looking device because it is the source of truth for admission.

The one genuine residual risk: a rogue relay operator could selectively drop messages for one targeted pair, indistinguishable from ordinary packet loss. **Fix, folded into `adr/0003-rendezvous-relay-split.md`:** the published rendezvous set must have more than one relay, with node-agent failover across the whole set — a requirement that wasn't previously stated (the ADR only said "a small set," without specifying what happens if one member of it misbehaves). Applied to `03_ARCHITECTURE.md` ("Signaling, stated precisely"), `05_BUILD_PLAN.md` Gate 1.2 (now scoped explicitly to admission-integrity, with the availability caveat and failover requirement), and `09_JUDGING.md` objection 14.

### ✅ RESOLVED — Gate 0.2 is not de-risked by having relay infrastructure available

Having a funded cloud account to run a relay on guarantees the relay *can run*; it does not guarantee two peers on two genuinely different networks actually complete a tunnel through it. `10_DAY0_GATES.md` Gate 0.2 is unrun until it is actually executed and measured — this is the only gate whose failure (K1) ends the project outright, and it should not be treated as solved by infrastructure availability alone.

**Run and measured Sept 6, 2026.** Laptop (home NAT) to a fresh AWS EC2 instance, real ENS-resolved admission on both sides (two independently registered on-chain devices, not a stand-in), real dumb relay, real bidirectional ping and one real TCP round trip. Full writeup in `10_DAY0_GATES.md` Gate 0.2. K1 does not apply; the topology tested is the easier one (VPS has a public IP) — the harder two-NAT case remains genuinely open, see the next finding.

### A real protocol bug the relay-mediated exchange exposed, found only by testing both directions racing

While writing the relay client (`brambled/rendezvous`), a two-sided candidate exchange test failed intermittently: whichever side received the other's candidate *first* would return immediately, closing its connection — but if that side's own first offer had raced ahead of the peer's registration (a near-certainty when both sides start at roughly the same instant) and gotten an immediate "not registered" error, it would never get retried. The second side would then hang forever waiting for an offer that was never resent. Not a timing edge case that only shows up under artificial delay — it reproduced on the very first real two-machine run's startup ordering too, before the fix.

**Fix:** the relay now acks a successfully forwarded offer (`{"type":"ack"}`), and `Exchange` doesn't return until it has *both* received the peer's candidate *and* had its own offer acked — so a side can't vanish mid-retry and strand the other. See `relay/main.go`'s protocol comment and `brambled/rendezvous/client.go`. Caught by a unit test before the real run, not by the real run itself — worth noting as a case where the CI-safe loopback test earned its keep.

## Open items for whoever reads this next

- ~~Resolve the Blocky402-vs-starter-kit-facilitator question before Gate 0.4~~ — resolved Sept 9: moot. Gate 0.4's proof (`scripts/gate0.4-blocky402-check/`) calls Blocky402's hosted testnet facilitator directly and never goes through the Scaffold-HBAR starter kit at all, so the starter's bundled facilitator's identity never mattered. See `10_DAY0_GATES.md` Gate 0.4.
- ~~Check whether `wallet-cli send` accepts arbitrary calldata~~ — resolved Sept 7, see below.
- ~~Whether `wallet-cli ring` works under Speculos specifically~~ — resolved Sept 8: it doesn't, no public Ledger CLI exposes Key Ring over Speculos. See item 2 above and `adr/0001`.
- When building Phase 4, make sure the demo's chosen topologies include at least one that can't hole-punch, so the data-relay path (not just rendezvous) gets shown live.
- The two-NAT case (STUN/hole-punching) is still unproven — Gate 0.2's real run only exercised the easier laptop-to-VPS topology. Needed before claiming general NAT traversal, not before Gate 0.2 itself (which explicitly blesses this topology as valid).
- ~~Append a `## Ledger` section here once "Ledger tracks explained" airs~~ — done, see below.

## ✅ RESOLVED — `adr/0001`'s both open questions: headless Key Ring confirmed, `send` calldata confirmed

Watched Sept 7 (local whisper.cpp transcript + slide frames from the video — full writeup in `docs/sources/ledger_workshop_clean.md`). This is the strongest possible primary-source answer to Gate 0.1's "entire premise of the Ledger enrollment story" sub-check: not a side comment, but the *stated design target*, in the speakers' own words, restating the hackathon's own suggested direction back at attendees: *"obviously ledger devices need to be plugged into your machine in order to work. So the idea is to implement key ring basically in a machine where you can't. So this can be a VPS, you know, CI runner, anything hosted that doesn't have direct access to your device."* Corroborated by the product slide (*"Headless by design: CI and agents decrypt with nobody at the keyboard"*), a live demo (device disconnected after provisioning, agent kept decrypting), and a direct, detailed Q&A answer walking through the actual mechanism (device provisions a machine's rights once; decryption afterward flows through the decentralized Trust Chain, no device or on-device confirmation involved).

**New nuance, not previously documented:** the provisioned machine's decrypt path has one more local factor — *"there's also like a password that's tied to your machine's OS."* Not zero-friction, but still fully headless (no Ledger device or human-in-the-loop confirmation needed post-provisioning).

**What the workshop itself does not resolve:** `adr/0001`'s calldata question (`wallet-cli send` signing arbitrary contract calls, e.g. `grantRoles`/`revokeRoles`) — not mentioned anywhere in the talk. **Resolved separately, immediately after, by just running `wallet-cli send --help`**: `send` takes a `--data` flag ("EVM calldata as 0x-prefixed hex"), confirming it is not transfer-only. Installed version was 2.1.0, not the v1.0.1 `04_TECH_STACK.md` had pinned — flags did drift, as that table's own warning anticipated. Both halves of `adr/0001`'s open verification are now closed; see that ADR.

**Caution — this is design confirmation, not Gate 0.1's actual test.** Gate 0.1 itself still requires running `ring init` on a laptop with the physical device, `ring encrypt`, then `ring decrypt` on a second machine with the device absent, for real. This finding removes the *risk that the feature doesn't work this way at all* — it does not substitute for actually running it once the device is on hand.

**Applied:** `10_DAY0_GATES.md` Gate 0.1's status updated; `docs/sources/ledger_workshop_clean.md` added (transcript + slide-derived notes, corrections table for ASR mis-hearings). New judging-criteria bullets from the workshop's own "What we like" slide, not previously captured anywhere, are in that file too — worth folding into `09_JUDGING.md` if a Ledger-specific objections section gets added there.

---

## Event schedule — submissions close Sept 13, 16:00 UTC / 21:30 IST, not Sept 16

Verified Sept 7 against the live ETHGlobal pages (`ethglobal.com/events/ethonline2026` and its `/prizes` page). The event JSON lists `submissionDeadline: 2026-09-13T16:00:00.000Z` — Sept 16 is the **finale**, after two judging rounds. `05_BUILD_PLAN.md`'s original Phase 6 window (Days 10–12 = Sept 13–15) was scheduled entirely after the deadline.

**Full schedule (UTC / IST):** signup closed Sept 6 17:00 / 22:30 · **Check-in #1 Sept 8 03:59 / 09:29** · Feedback Sessions Sept 8 18:00 / 23:30 and Sept 10 13:00 / 18:30 · **Check-in #2 Sept 11 03:59 / 09:29** · **Submissions Sept 13 16:00 / 21:30** (`requireVideoSubmission` is on — video due with the showcase, no post-deadline window) · Judging Round 1 (async) Sept 13 19:00 UTC / Sept 14 00:30 IST · Round 2 (live) Sept 14 16:00 / 21:30 · Finale Sept 16 16:00 / 21:30.

**Applied:** calendar + phase dates rewritten in `05_BUILD_PLAN.md`; key-dates table + commit-cadence status added in `08_EVENT_RULES.md`; risk-register rows added in `07_RISKS.md`; Gate 0.1/0.4 urgency notes in `10_DAY0_GATES.md`; event line in `00_START_HERE.md`.

## Track pages verified verbatim (Sept 7) — three deltas worth acting on

The three target tracks (ENS "Best Use of ENSv2", Ledger "AI Agents x Ledger", Hedera "AI & Agentic Payments") read exactly as `02_TRACK_FIT.md` and this file already describe them — quoted phrases, prize amounts, and qualification text all match the live pages. Ledger's and ENS's second prizes ("Continuity", $1,500; "Best Integration of ENSv2", $500) are **Continuity-track only** and correctly not targeted. Deltas:

- **Hedera's extra-points list is longer than documented.** Besides metering, ERC-8004 identity, agent discovery, and HCS audit trails (already known): **HTS tokens or custom fee schedules in the settlement path**, **recurring/streamed payments via Scheduled Transactions**, and **A2A/ACP** multi-agent negotiation. The first two are simple add-ons for the relay settlement path (batch-settle accumulated usage via a Scheduled Transaction). Folded into `02_TRACK_FIT.md`.
- **"Ledger Tracks Explained" aired Sept 7, 14:00 UTC / 19:30 IST** (recording linked from the event schedule page). Still to be watched; it is the most likely source to resolve the `wallet-cli send` calldata question (`adr/0001`'s open verification) before Phase 3 starts Sept 9.
- **Project Check-ins and Feedback Sessions are ETHGlobal showcase requirements** (Sept 8 and Sept 11, 03:59 UTC / 09:29 IST), tracked in `05_BUILD_PLAN.md`'s calendar and `07_RISKS.md`'s register.
