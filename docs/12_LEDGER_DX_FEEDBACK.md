# Ledger Developer Experience Feedback

Written for Ledger's own submission requirement (`developers.ledger.com/ethonline`, Track 01): "Developer Experience (DX) feedback documentation including gaps, confusing flows, and specific improvement suggestions." Everything below came from real hands-on testing against real hardware (Nano S Plus, `wallet-cli` v2.1.0) on Sept 10, not from reading docs. Full raw transcript: `.tmp/LEDGER_GATE0.1_REPORT.md` (private working notes — not part of this submission, kept internal).

## What worked well

- `genuine-check`, `ring init`, `ring encrypt`/`decrypt`, and `account discover` all did exactly what their `--help` text says, on the first or second try. The CLI's JSON output mode (`--output json`) is genuinely pleasant to script against — structured `{ok, data}` / `{ok, error}` envelopes throughout, no ad-hoc text parsing needed.
- `send --help`'s `--data` flag (arbitrary EVM calldata, not just native transfers) is exactly what a project like this needs to sign contract calls, and it's documented clearly once you look. **Caveat, Sept 11: this only works on mainnets — see Gap 4.**
- Error messages are actionable, not just descriptive — e.g. the missing-`WALLET_PASS` error prints the exact `security find-generic-password` / `secret-tool` command to fix it, for the right OS.

## Gap 1 — `ring init` requires the **Ledger Sync** app to be open, but nothing says so

`ring init` failed with a generic `"An unknown error occurred talking to the Ledger."` the first time we ran it, despite the device being genuine, unlocked, and on its dashboard. The actual fix — installing and opening the separate **Ledger Sync** app via Ledger Live — took a manual back-and-forth to discover, because:
- `ring --help` never names an app requirement.
- The error message is generic (`command-execution` / `CommandExecutionError`) and gives no hint that the problem is "wrong app open on device," as opposed to a USB/permissions/genuine-check issue.

**Suggestion:** have `ring init` detect "device connected but Ledger Sync app not open" as a distinct, named error state, and say so directly (the same way the `WALLET_PASS` error names its own fix precisely).

## Gap 2 — the biggest one: "headless by design" doesn't hold for a genuinely fresh host

Ledger's own materials describe Key Ring as "headless by design: CI and agents decrypt with no human at the keyboard," and name enrolling "a VPS, a CI runner, or a hosted agent" as the explicit intended use case. We tested this literally, end to end, on a real second machine — not a simulation:

1. Ran `ring init` + `ring encrypt` on a MacBook, device present. Worked.
2. Copied the two local state files (`session.yaml`, `first-run.json` — the only files `wallet-cli` writes locally) to a real, separate Ubuntu ARM64 EC2 instance, installed `wallet-cli` there fresh, and attempted `ring decrypt`, device never attached to that host at any point.
3. **Failed**: `"Member credentials not found in the OS keychain. Run wallet-cli ring destroy then wallet-cli ring init to reset."`
4. Suspected this was just a missing Secret Service daemon on a headless box (the EC2 instance had no `gnome-keyring`/`secret-tool` at all initially). Installed `gnome-keyring` + `libsecret-tools`, ran the attempt again inside a scoped `dbus-run-session` with a real keyring daemon live. **Failed identically.**

That rules out "needs a keyring daemon" as the cause. The real finding: the two files `wallet-cli` writes locally (`session.yaml`, `first-run.json`) contain no secret material at all — just the public trustchain root ID, a password salt, and key-usage metadata. The actual member credential lives somewhere `wallet-cli`'s CLI surface never exposes an export/import path for (checked `--help` on every `ring` subcommand — `init`/`encrypt`/`decrypt`/`keys`/`destroy` — none offers one).

**What this means in practice:** "headless" currently means *the device isn't needed for decrypt calls on the machine that already ran `ring init`* — not *"hand a fresh VPS some ciphertext and it can just decrypt it,"* which is what "enroll a VPS with no USB port" reads as. A brand-new host genuinely cannot decrypt anything via any mechanism the shipped CLI exposes, until it either (a) has the physical device attached to it at least once, or (b) some other, currently-undocumented provisioning step transfers real member credentials to it.

**We checked whether (b) exists before writing this up, so this isn't a guess from one unusual test.** We cross-referenced five independent Ledger-published sources — the developer portal's `wallet-cli` page, the `apps/wallet-cli` README, the PR that added the `ring` command group, the LKRP whitepaper repo, and (closest to authoritative) [Ledger's own blog post on how Ledger Live's device-sync feature actually uses LKRP](https://www.ledger.com/how-we-used-ledger-key-ring-protocol-in-ledger-live). That post states plainly that adding a *second* Ledger Live instance to an existing Key Ring requires "the same device or a device seeded with the same recovery phrase," and that the new instance "will go through the same two steps on the Ledger Device" to join — i.e., in Ledger's own flagship shipped use of this exact protocol, every member independently authenticates via hardware. None of the five sources mention any device-free join path (invite code, QR-equivalent, remote-approval flag) for a brand-new member. So (b) isn't just undocumented — the one place Ledger does document member enrollment in detail says the opposite of (b). This reads as confirmed protocol behavior, not a `wallet-cli`-specific gap or a quirk of our test rig.

The workshop Q&A ("Ledger Tracks Explained," Sept 7) actually describes something closer to reality, if read literally: *"the agent contacts a computer server somewhere, that computer has the CLI ring built into it... that machine then does the ledger encryption/decryption... sends [a] response."* That's a **decrypt-as-a-service** model — one already-provisioned machine keeps doing the decrypting, and other hosts' agents call it over the network — not "ciphertext ships anywhere and decrypts anywhere." That distinction isn't obvious from the marketing copy or from `--help` text, and it's a meaningfully different thing to build against.

**Suggestions:**
- State explicitly, wherever "headless by design" is claimed, which of the two models is actually supported: same-host-forever, or genuine cross-host handoff. If it's the former, say so plainly — it's still a strong pitch, just a narrower one.
- If genuine cross-host credential handoff (option b above) is intended to be possible, expose it as an actual CLI verb (`ring export` / `ring import`, or similar) rather than leaving it as an undocumented gap between two files that look complete but aren't.
- If it's meant to be the decrypt-as-a-service model, a minimal reference implementation (a tiny daemon wrapping `ring decrypt` for a remote caller, with its own auth story) would remove a full day of guessing for anyone building the "enroll a VPS" use case Ledger names as the flagship scenario.

## Gap 3 — the local password (`WALLET_PASS`) has no setup command

First run of any `ring` command demands `WALLET_PASS` be set (env var or OS keychain), but no `wallet-cli` command sets or initializes it — you're expected to just pick a value and supply it via `security add-generic-password` (macOS) or `secret-tool store` (Linux) yourself, worked out from reading the error message rather than any onboarding flow. A `wallet-cli auth set-password` (or similar) that writes it to the right OS-native store for you would remove this step entirely.

## Gap 4 — `send` on a testnet fails with an opaque `FeeNotLoaded`, not "testnet sends unsupported"

We built our ACL-escalation demo (an admin approving a widened agent permission, physically confirmed on-device) around `wallet-cli send --data <calldata>` signing a Sepolia testnet transaction. It never reached the device. Every attempt — a plain 0 ETH transfer with no calldata at all, `--dry-run` and without — failed identically and immediately, before any on-device prompt:

```
{"ok":false,"error":{"kind":"command-execution","name":"CommandExecutionError","message":"FeeNotLoaded","command":"send"}}
```

`account discover ethereum:sepolia` and `balances` both worked correctly against the same account first (real address, real 0.05 testnet ETH balance confirmed) — so this isn't a broken account or network mismatch. We traced `FeeNotLoaded` into `ledger-live`'s own source (`libs/ledger-wallet-framework/src/errors.ts`, referenced from `coin-evm/src/prepareTransaction.ts`) to confirm it's a real, intentional error class (fee/gas data never populated), not a crash — but nothing in `send --help`, the error itself, or `ring`/`send`'s own docs says *why* fees never populate here.

The actual answer exists, but only in one place: Ledger's own `agent-skills` repo, `skills/wallet-cli/wallet-cli-usage/SKILL.md`'s "Out of scope" section states plainly that `send`/`receive`/`operations`/`swap execute` on testnets and L2s are not supported yet. That's the right information, but it's scoped to an AI-agent skill file a human developer reading `wallet-cli --help` or the package README would have no reason to find. We only found it by searching Ledger's GitHub org broadly after independently ruling out every other explanation (bad account, bad network, sandbox networking, our own calldata).

**Impact for this project specifically:** the intended demo shot — a human approving an agent's widened permission with one physical button press, the actual on-chain grant, live on Sepolia — isn't achievable with shipped `wallet-cli`. We kept the Ledger-gated part of that flow (the granter's private key is decrypted from `wallet-cli ring`, a real on-device-rooted step) but had to fall back to software signing for the transaction broadcast itself, since re-implementing EIP-1559 signing against `@ledgerhq/hw-app-eth` directly, untested, two days before submission, was a worse risk than an honest scope note.

**Suggestions:**
- Have `send` (and `receive`/`operations`/`swap execute`) fail fast on a testnet/L2 account with a named, specific error — `TestnetSendNotSupported` or similar — instead of proceeding into fee estimation and failing there with a generic message that looks like a bug in the caller's transaction, not a scope boundary.
- Say it in `send --help` and the package README, not only in the AI-agent skill file — a developer reading the CLI's own docs currently has no way to learn this without trial and error.
- If testnet support is on a roadmap, even a rough timeline would have changed our architecture decision going in, rather than costing a day of debugging plus a build/revert cycle discovered only after `account discover`/`balances` had already worked and looked fully supported.
