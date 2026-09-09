# ADR 0001 — Split Ledger key custody (`ring`) from Ledger signing (`send`)

**Status:** Decided, Sept 4 2026. Verified against `@ledgerhq/wallet-cli` v1.0.1 docs.

## Context

The Ledger track requires building on the Ledger Agent Stack, specifically `wallet-cli ring`. It would be easy to assume `ring` is a general-purpose signing tool, since Ledger devices are best known for signing. It is not: `ring init / encrypt / decrypt / keys / destroy` is LKRP-backed **encryption** of files and text, keyed to wallet-sync membership. It does not sign transactions.

Two distinct jobs exist in this project that both touch the Ledger:

1. Storing a headless node's WireGuard private key at rest on a host with no Ledger attached.
2. Confirming, on-device, that a human authorized an enrollment or revocation before it reaches the ENS registry.

## Decision

- **Encryption of the node's private key at rest** → `wallet-cli ring`. The key is encrypted under the ring on a machine where the Ledger *was* present at ring-creation time, then the ciphertext ships to the headless host, which decrypts without the device attached (this is the entire premise of the Ledger enrollment story — see `12_SOURCE_NOTES.md` for its verification status).
- **Signing the ENS registry transaction that grants/revokes/rotates a role** → `wallet-cli send`, which requires physical on-device confirmation for every call.

Never describe `ring` as signing anything. A Ledger judge will catch it (see `10_JUDGING.md` and Gate 3.3 in `05_BUILD_PLAN.md`, which greps for exactly this claim).

## Known limitation

The ring's domain key derives from the wallet-sync encryption key, which LKRP rotates whenever a ring member is removed. After rotation, ciphertext encrypted under the old key can no longer be decrypted. Do not build a flow that depends on decrypting old ciphertext after a membership change — re-encrypt affected key material at rotation time instead.

## ✅ Both halves now verified (Sept 7)

**`send` accepts arbitrary calldata — confirmed directly from `wallet-cli send --help`** (installed version 2.1.0, not the v1.0.1 `04_TECH_STACK.md` pinned — see its version-drift note): a `--data` flag takes "EVM calldata as 0x-prefixed hex (e.g. `0xd0e30db0`)", alongside the native-transfer flags (`--to`, `--amount`). This is not transfer-only — `send --to <registry> --data <encoded grantRoles/revokeRoles call>` is the real signing path for Phase 3's enroll/revoke/rotate transactions, with on-device confirmation per `--device-timeout`. Not resolved by Ledger's Sept 7 "Tracks Explained" workshop (the talk never mentions calldata) — resolved instead by just running `--help`, exactly as this file's action item said to.

**The `ring` half of this decision is now confirmed by primary source too**, not just the docs: Ledger's own product team named "a VPS, CI runner, anything hosted that doesn't have direct access to your device" as `ring`'s explicit intended target, demoed decrypting with the device disconnected, and walked through the mechanism in Q&A — see `12_SOURCE_NOTES.md`'s "✅ RESOLVED" entry and `docs/sources/ledger_workshop_clean.md`. `wallet-cli ring --help` corroborates independently: only `init` is marked "(device required)"; `encrypt`/`decrypt`/`keys`/`destroy` are not. One added detail for implementation: the provisioned machine's decrypt path also checks a password tied to that machine's OS, on top of the Trust Chain authorization — plan for that as a real input the enrollment/provisioning flow needs to supply or prompt for, not assume decrypt is a bare no-auth call.

**What's still actually pending is the physical run**, not the design questions: `ring init`/`encrypt` with a real device present, `ring decrypt` on a second machine without it.

## Speculos: actually tried it (Sept 7) — the transport just isn't wired into this binary

Don't take the paragraph below on faith either — it's the result of actually running it, not a theory. Sequence:

1. Compiled `wallet-cli`'s embedded config schema has real `SPECULOS_API_PORT`/`SPECULOS_DEVICE`/`SPECULOS_SEED`/`SPECULOS_FIRMWARE_VERSION` keys, flagged internally `"(dev feature)"`.
2. `LedgerHQ/ledger-live`'s own docs confirm their LKRP e2e suite genuinely records scenarios against a real Speculos container running a purpose-built **Ledger Sync** app (`LedgerHQ/app-ledger-sync`, public source) — *"against a Speculos device (Docker) and the staging trustchain backend"* (`docs/ledger-sync/test-strategy.md`).
3. Built that exact app from source with Ledger's own `ledger-app-dev-tools` Docker image (succeeded — real `app.elf`), ran it under Speculos (loaded correctly, confirmed via logs: `Env app name: 'Ledger Sync'`).
4. Pointed `wallet-cli ring init` / `genuine-check` at it via `SPECULOS_API_PORT`/`SPECULOS_DEVICE`, tried both the API and raw APDU ports, tried adding `MOCK=1`.
5. **Every attempt failed identically**: `No Ledger device found. Unlock the device and try again` — the plain USB-scan failure, not a Speculos- or attestation-specific error.

Checked why: the binary's device-discovery code only ever constructs `node-hid`/`webusb` transports — no trace of the `speculosTransportFactory` call the DMK library it's built on actually exposes for this. The `SPECULOS_*` config keys read as inherited/vestigial (almost certainly copied from the same shared internal config schema the LKRP e2e recorder uses) rather than genuinely wired into this compiled tool's transport selection. So the earlier "would dead-end at attestation" prediction was never actually reached — it fails one step earlier, at plain device discovery, for a mundane reason (missing wiring) rather than the security-boundary one originally guessed.

**Closing this for good:** `apps/wallet-cli`'s source is public (MIT, `LedgerHQ/ledger-live`), so wiring in DMK's own `speculosTransportFactory()` and building a patched binary is technically possible. Not worth doing. It would only get past *discovery* — `ring init` would still need to clear production Trust Chain's secure-element attestation check, which is the wall the first theory named and nothing since has removed (Ledger's own recording runs against a **staging** backend per `docs/ledger-sync/test-strategy.md`; the app's `PROD_PRIVATE_KEY?=0` toggle and Speculos's `--attestation-key` flag both point the same direction: test attestation only works where a backend is configured to accept test attestation). Building the transport is effort spent reaching a wall faster, not through it. **`ring init` needs real hardware — final, three checks deep, stop re-deriving this.**

**Sept 8 — checked again against Ledger's own Discord guidance, unchanged, cleaner citation now available.** Ledger's Discord support pointed hackathon participants (including one asking specifically about Wallet CLI hardware requirements) at the generic device-app-framework Speculos docs. That page is about building/testing your own on-device app with Ragger — unrelated to Key Ring. Checked whether it changes anything by reading both CLIs' own READMEs directly (installed `wallet-cli` 2.1.0 — current latest per npm — and `@ledgerhq/live-cli`'s README on `LedgerHQ/ledger-live`, published the same day this was checked):

- `wallet-cli`'s own README states it plainly: `@ledgerhq/live-cli` "is now an internal, Speculos-only tool used by the monorepo's e2e suites and CI" — implying `wallet-cli` itself is not.
- `live-cli`'s README confirms the split from its side and is explicit about scope: "It is **Speculos-only**: it does not ship USB/HID or HTTP-proxy transports." Its entire command surface is fixed and small — `getAddress`, `liveData`, `send`, `tokenApproval`, `tokenAllowance`, `generateAddresses`, `generateUtxoAddresses`, `generateAppJson`, `version` — built for `live-common` e2e flows. **No `ring`/Key Ring/LKRP command exists in it.**

So: no publicly available Ledger CLI exposes Key Ring over Speculos. `wallet-cli` has `ring` but is hardware-only by its own README; `live-cli` is Speculos-only but doesn't have `ring` in its fixed command set at all. This does not prove Key Ring is incapable of running under Speculos in principle — Ledger's own LKRP e2e suite reportedly does run against Speculos plus a **staging** trustchain backend (see above) — it proves no shipped tool currently lets a hackathon participant reach that staging path. The wall is access to a staging backend + the right client wiring, not the emulator itself.

**One cheap lever left, not yet pulled:** that Discord thread is a live line to a Ledger employee, and the original question was specifically about Wallet CLI. Worth asking directly: *"`wallet-cli ring init` is marked `(device required)` and only constructs node-hid/webusb transports — is there a Speculos path for Key Ring/LKRP, or a staging trustchain endpoint hackathon participants can point at?"* Until that's asked and answered, the conclusion stands: **`ring init` needs real hardware.**

## Threat model for the decrypted key, stated precisely (Sept 8)

Against the common leak class — a stolen disk, backup, log, or git history — Key Ring answers cleanly: ciphertext only, nothing usable. The one real caveat is code execution *inside the process holding the decrypted key*. That caveat is narrower than "anything compromises this host": `brambled` and "the agent" (`03_ARCHITECTURE.md`'s "agent path") are separate processes, talking over a local boundary, not one binary. A compromised agent — a poisoned dependency, a prompt-injected tool call — never shares `brambled`'s address space, so it can't read the decrypted key directly; it would need a second, separate privilege escalation to cross that boundary. **Implementation discipline this depends on:** never link `brambled` in as a library inside the agent's own binary. Doing so collapses the separation this argument rests on.

## Known limitations, left for future work (Sept 8)

Two open questions, deliberately not solved in this timeline:

- **Why hardware, not a cloud KMS/HSM?** The honest differentiator is provenance — a human physically confirmed the provisioning event, outside any cloud provider's own blast radius — but that argument has to be stated explicitly in the pitch, not assumed.
- **Provisioning doesn't scale to a large agent fleet yet.** Each new headless host needs one human-confirmed Ledger step. Fine for a demo's single agent; batching or pipelining this for many concurrent CI jobs is unbuilt.
