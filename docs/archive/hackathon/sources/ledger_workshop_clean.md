# Ledger Workshop — Etienne Waldron & Oscar Chaix, Ledger Product Managers (AI initiatives)

**Source:** https://www.youtube.com/watch?v=a06GK98Fmqw — "Ledger Tracks Explained | Etienne Waldron & Oscar Chaix at ETHOnline 2026" (~29.5 min)
**Method:** local ASR, whisper.cpp (`ggml-medium.en`) against the extracted audio track — no official captions were available for this one. Supplemented with ~50 slide frames (scene-change + uniform sampling) pulled from a 240p copy of the video, read directly, to catch text the audio alone wouldn't disambiguate (e.g. exact slide bullet wording, the QR/URL, prize figures).
**Confidence:** speakers are Ledger's own AI-initiative product managers — track/prize/product facts here are primary-source. Proper nouns are ASR-derived and corrected only where the mis-hearing was unambiguous (usually because the same term also appeared, spelled correctly, on a slide).

## Corrections applied

| Heard | Corrected to | Confidence |
|---|---|---|
| Elektra (company name in the intro) | Ledger | High — matches slide text and the rest of the transcript |
| Etienne Woljian | Etienne Waldron | High — matches the event schedule page |
| Oskar Schechts | Oscar Chaix | High — matches the event schedule page |
| "check for the recipients on same history" | "check the recipient's **on-chain** history" | High |
| "fighter 2FA" | **FIDO2** 2FA | High |
| "feeder to device app" | **FIDO2** device app | High |
| Clubcode / cloud code | **Claude Code** | High — consistent mis-hearing across the whole demo section |
| "x42" (payments) | **x402** | High — dropped digit |
| "developer.ledger.com/ethonline" (spoken) | **developers.ledger.com/ethonline** (slide/QR text) | High — trusting the written URL over ASR |
| "matchings that you yourself provision" | "**machines** that you yourself provision" | Medium |
| "Blockade and Savers" (transaction-simulation providers) | possibly **Blockaid** and one other provider; second name not confidently resolved | Low |
| "Nanogen five" (device model) | unresolved — no confirmed Ledger device by this name; possibly a mis-hearing of a Nano model reference | Low, do not cite |
| "Maquis from CAN" (Q&A, an attendee's question about a prior demo) | unresolved, likely a specific past hackathon project name | Low, do not cite |

Attendee names/handles (Alex, Anurag, "ROM reactor") left as heard.

---

## Talk, by section

**Company background** (slide-confirmed): Ledger, founded in France in 2014. ~20%+ of world crypto assets secured, 8M+ devices sold. Security model: a secure element chip plus a screen that sits directly on top of it, so what's displayed cannot be tampered with — "what you see is what you sign."

**Why this applies beyond crypto custody — the agentic-era pitch.** Slide: *"Anything that can act for you can be turned against you."* Three named risks: **prompt injection** ("a page, a tool response, or a peer agent rewrites the instruction, and the funds move"), **hallucination** ("the model is confident about the wrong address, the wrong amount, the wrong chain"), **leaked secrets** ("API keys and seeds live in plaintext `.env` files the agent can read and forward"). Ledger's proposed model: agents *propose* actions, a human *approves*, hardware *signs* — the private key never leaves the secure element, and anything critical needs a physical human confirmation.

**The Ledger Agent Stack — three pillars** (slide-confirmed, exact bullets):

1. **CLIs for the Ledger apps** — Wallet, Enterprise, and Enterprise Multisig, driven from a terminal or any agent with terminal access (Claude Code, Codex, etc.). Examples given: *"Lock in 20% of my BTC as USDC and Clear Sign on device,"* *"ETH is 70% of my wallet, target 40%. Propose the smallest set of swaps,"* *"Check this recipient's on-chain history, then queue the send."*
2. **Teach an agent to sign** — the Device Management Kit (DMK) SDK, plus skill files that teach any coding agent to wire Ledger signing into an app. Examples: *"Add Ledger Ethereum signing to my Vite + React wallet,"* *"Require a Ledger confirmation before my bot sends anything,"* *"Swap my dev signer for a real Ledger in the checkout flow."* Framed as turning a multi-day integration into "10 minutes... just ask your agent."
3. **Key Ring: beyond crypto** — *"Secrets, not coins. Encrypted under keys derived from your Ledger seed, one device tap to set up, then none."* *"Decrypt on any machine, recoverable from your seed. No vault to host."* *"Headless by design: CI and agents decrypt with nobody at the keyboard."* Also ships OpenPGP and FIDO2 device apps for hardware 2FA.

**✅ Directly and repeatedly confirms Gate 0.1's headless sub-check.** This is the single most load-bearing finding for bramble. Stated as the *explicit intended use case*, not an edge case, in at least four separate places:

- Slide, verbatim: *"Headless by design: CI and agents decrypt with nobody at the keyboard."*
- Track pitch (Waldron, restating the hackathon ask directly): *"obviously ledger devices need to be plugged into your machine in order to work. So the idea is to implement key ring basically in a machine where you can't. So this can be **a VPS, you know, CI runner, anything hosted that doesn't have direct access to your device**."* — this is bramble's exact scenario, named outright.
- Live demo (Chaix): after provisioning the Key Ring once with the device present, *"from now on, I don't really need my ledger device, my agent can decrypt and encrypt access tokens."*
- Q&A, asked directly and answered in detail (worth quoting at length): *"the ring CLI... works on the key ring protocol that we also use at Ledger... that doesn't happen with a device plugged in and formal validations on the device. It happens through the trust chain with a simple web connection. So what actually happens is that you use your device to provision a certain machine with the rights to encrypt/decrypt those secrets... your ledger hardware serves as the root for that authorization. But then once you're initialized, the agent can decrypt or encrypt autonomously."*

**Nuance the Q&A adds, not previously in bramble's docs:** the provisioned machine's decrypt path isn't zero-friction — *"there's also like a password that's tied to your machine's OS"* as an added local factor. And the flow, restated end to end: *"the agent contacts a computer server somewhere, that computer has the CLI ring built into it, connecting with the trust chain. So the agent is simply just asking the machine... 'here I have the approval from the user to do something'... that machine then does the ledger encryption/decryption, does whatever the agent requested, sends [a] response."* The Trust Chain itself is described as **decentralized**, and secrets are encrypted client-side by the Ledger hardware before ever reaching it — Chaix, in Q&A: *"because your secrets are encrypted via your ledger on your machine, they're not accessible through that trust chain directly... it's all linked to your ledger hardware."*

**❌ Does not resolve the `wallet-cli send` arbitrary-calldata question.** No mention anywhere in this talk of calldata, `grantRoles`/`revokeRoles`, or signing an arbitrary contract call — every CLI example shown is a built-in wallet operation (balance check, swap, stake, send). `adr/0001`'s open verification item is still open; check `wallet-cli send --help` directly rather than expecting this workshop to answer it.

**Live demo, Key Ring specifically (Chaix, narrated, 2x speed).** Asked Claude Code to encrypt an Anthropic API key via the Key Ring CLI. No ring existed yet on the machine — the agent recommended pasting the secret via clipboard so the agent's own context never sees it (an alternative path exists too, and the demo used a plaintext file directly for the demo's sake). The CLI encrypted it, deleted the original plaintext file, and confirmed (asked again, for the demo) that the raw key never appeared in the agent's history or memory — only a decrypt-and-use-once flow, verified live against a real Anthropic API call (200 status).

**Tracks.**
- **AI Agents x Ledger** — start something new during the hackathon. **$2,000 / $1,000 / $500** for 1st/2nd/3rd (three winners). Four suggested directions (per `02_TRACK_FIT.md`, already captured): Key Ring on a headless host, agents using scoped secrets without holding raw credentials, human-in-the-loop approval for high-risk actions, and agents paying for what they use (x402-style).
- **Continuity** — extend an existing project with the Agent Stack / DMK skills; does not need to have used Ledger before. **$1,500** across two prizes (exact split not stated on the slide shown).

**Judging criteria — new, not previously in bramble's docs** (slide, "What we like" + "Every submission includes documentation feedback"):
- Real user value, not generic chatbot wrappers.
- Concrete use of Ledger primitives, not just wallet branding.
- **"Something we can run without you in the room: a repo, or a recorded walkthrough."**
- Clear boundaries between autonomous behavior and explicit approval.
- Practical demos that show why device-backed trust matters for AI.
- For Continuity specifically: a clear before-and-after.
- Documentation feedback requested on submission (gaps, confusing flows, missing context) — bonus points for tutorials/code samples, nav/search improvements to their docs, "time-savers for the next integrator."

**Link:** developers.ledger.com/ethonline — tracks, prizes, and every doc link, via the QR code shown on the closing slide.

## Q&A highlights

- **Transaction Check** (fraudulent-contract simulation): uses multiple third-party simulation providers in parallel (unusual — most competitors rely on one), cross-checking each other. Results are rendered **on the hardware itself**, not the software/host side — deliberately, so a compromised front end can't fake or suppress a fraud warning. Optional/dismissible — the user always stays in control of whether to sign despite a flag. Only available on touchscreen devices (Stax, Flex; a newer Nano generation was mentioned but the exact model name is unresolved, see corrections table) — older Nano S/S Plus/X users can access the same simulation only through Ledger's software playgrounds/simulators, not on-device.
- Ledger Nano X was confirmed, directly, as capable of initializing the Ring CLI and performing encryption/decryption.
- Bug reports / protocol questions for the Key Ring go to the group chat linked from the developer portal page, not GitHub issues or a security inbox specifically (direct messages to the team also work).
- Ring CLI vs. the OpenPGP device app were both raised as separate encryption primitives with different trust models: OpenPGP requires the physical device present (and optionally an on-device approval) for every operation; Ring, once provisioned, does not.

## Open items for whoever reads this next

- `wallet-cli send --help`, checked for a `--data`/calldata flag — still unresolved, still cheap, still blocking `adr/0001`'s second half.
- Confirm the exact Continuity-track prize split ($1,500 across two places — which is which).
- If the project ends up needing the exact vendor names behind Transaction Check (Blockaid + one other), verify rather than cite the ASR guess.
