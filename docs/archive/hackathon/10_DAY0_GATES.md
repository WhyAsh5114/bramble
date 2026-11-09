# Day 0 Gates

**Do these before writing any product code.** Two of them can kill the project. Report results before proceeding.

## Gate 0.1 — Is there a physical Ledger device? (15 minutes)

The Ledger track requires the Agent Stack, and specifically `wallet-cli ring`. `ring init` requires a Ledger device over USB. Sub-check: confirm that a ring initialised on a laptop can be used on a headless host **without the device attached**, because "bring the Key Ring to hosts with no USB port" is the whole enrollment story.

- **No device available** → the Ledger slot is dead. Do not attempt it. **Report immediately**; this changes `02_TRACK_FIT.md`.
- **Device available but the headless flow does not work** → this is the actual outcome (see Status below), but "drop the Key Ring claims entirely" turned out to be the wrong resolution — retarget Key Ring at a secret that's same-host by design (the admin's ACL-granter key, `adr/0005`) instead of dropping it. See `adr/0001`'s "Pivot (Sept 10)" section for the full reasoning.

**Test:** `wallet-cli --version` returns v1.0.1 or later; `wallet-cli ring init` completes; `ring encrypt` on the laptop and `ring decrypt` on a second machine with no device attached.

**✅ RESOLVED Sept 10, 2026 — physical run complete.** Real Nano S Plus, `wallet-cli` v2.1.0. `genuine-check`, `ring init` (real Trust Chain), and `ring encrypt`/`decrypt` all worked, including decrypt with the device physically unplugged (confirmed absent from USB, not assumed) on the machine that ran `init`. The second half of the test — `ring decrypt` on an actual second machine with no device ever attached there — was also run for real, against the `Bramble` EC2 instance, and **failed**: no mechanism `wallet-cli` exposes transfers a member's decrypt capability to a new host, even with a full local Secret Service (`gnome-keyring`) installed there to rule out that as the cause. This corrects the Sept 7 workshop-derived optimism below — Ledger's own "headless by design... enroll a VPS with no USB port" framing means *decrypt without the device on the already-provisioned machine*, not *decrypt anywhere ciphertext is sent*. Full transcript: `.tmp/LEDGER_GATE0.1_REPORT.md`. `wallet-cli send --help` (`--data` accepts 0x-prefixed EVM calldata) was already confirmed Sept 7 and still holds. **Resolution:** per the retargeted bullet above, Key Ring now protects the admin's own ACL-granter key (`05_BUILD_PLAN.md` Gate 3.2, `adr/0001`) rather than the node's WireGuard key — same-host by design, so this finding doesn't affect it.

**Original Sept 7 note, kept for the record:** the Ledger "Tracks Explained" workshop (transcript + slides in `docs/sources/ledger_workshop_clean.md`) named "a VPS, CI runner, anything hosted that doesn't have direct access to your device" as Key Ring's intended use case and demoed decrypting with the device disconnected — true as far as it went, but (per the Sept 10 run above) that demo's headless machine had itself already run `ring init`; the workshop didn't show a *never-provisioned* host decrypting.

**Device note:** ran this gate against a dedicated device, not hardware holding real funds — `ring` is new/beta functionality, and a wallet-level reset or passphrase profile doesn't rule out a bug below the seed layer (firmware, secure-element state). A Nano S Plus was sufficient (no touchscreen/Transaction Check needed for `ring`/`send` against Sepolia).

## Gate 0.2 — Two peers, two networks, no trusted coordinator (target: end of Sat Sept 5, hard deadline Sun Sept 6)

**This is the project.** Everything else is decoration on top of it.

Get a laptop on home WiFi and a VPS (or a phone hotspot) to establish a WireGuard tunnel where:

- Candidate exchange goes through a **dumb relay you wrote** that only forwards opaque blobs.
- Neither side consults any service that could authorize a peer.
- Direct connection succeeds via hole punching, or falls back to relay cleanly.

**Test:** `ping` and one TCP connection across the tunnel, from two genuinely different networks (not two processes on one machine, not two VMs on one host).

**If this is not working by end of Sunday Sept 6: stop and reassess whether to continue this project.** That decision must be made on Sunday. Discovering it on day eight means shipping nothing.

**Note:** a laptop-behind-home-NAT-to-VPS topology is the easier case — the VPS has a public IP and needs no traversal on its side. It is a valid and representative test (it matches the "agent on a VPS reaching a home lab" use case in `01_WHY.md`), but passing it is not proof of the harder two-NAT case. Treat this gate as unrun until it is actually executed and measured, regardless of what infrastructure is available to run it on.

**✅ VERIFIED Sept 6, 2026** — laptop (behind home NAT) to a fresh AWS EC2 instance (`ap-south-1`, public IP), the easier of the two topologies per the note above. `relay` (`../relay`) ran on the VPS, both sides ran real `brambled serve` builds (real OS TUN by default, not netstack) against the live Sepolia dev tailnet with two independently ENS-resolved devices (`device1` = laptop, `device2` = VPS, each with a freshly generated real WireGuard key registered on-chain via `scripts/provision-dev-tailnet`). Neither side was given the other's address directly — each learned it by exchanging opaque candidates through the relay, keyed only by public key. Confirmed: real bidirectional ICMP (`ping` laptop→VPS succeeded first; VPS→laptop succeeded afterward once WireGuard learned the laptop's NAT-mapped address from the first handshake — the "roaming" behavior `wgnode.AddPeer`'s doc comment describes) and one real TCP byte round trip (`nc`) laptop→VPS. **Two-NAT case is still unproven** — this run's topology is the easier one, per the note above; hole punching/STUN remain unimplemented (see `brambled/README.md`'s "Known gaps"). One real bug was caught and fixed while building the relay client: a race where the first side to receive the other's candidate could return before its own (earlier, race-lost) offer was ever retried, stranding the second side — fixed by having the relay ack successful forwards and having the client wait for both directions before returning.

## Gate 0.3 — ENSv2 Sepolia is usable (1 hour)

- **First:** point at the hackathon-specific ENSv2 deployment, not production — see `04_TECH_STACK.md`, "Hackathon ENSv2 Sepolia deployment." Overwrite viem/ethers' built-in Universal Resolver address per the snippets there before doing anything else in this gate — skipping this makes every later step silently wrong.
- Register a test name on ENSv2 Sepolia via the hackathon ENS App (registration fee is paid in MockUSDC with an unrestricted mint, so it is free).
- Create a subname with its own Permissioned Resolver.
- Write and read back a text record.
- Delegate one specific right via Enhanced Access Control and confirm an unauthorized account cannot perform it.

**Test:** the EAC delegation actually restricts. If a delegated account can edit records outside its grant, the ACL story changes and the ENS pitch needs rewriting.

**Note:** the hackathon deployment is pinned for the event's duration specifically so it doesn't move under you — use it, not the production/beta addresses, which ENS Labs has said may still change before mainnet.

**✅ VERIFIED Sept 5, 2026** — ran end-to-end against the live hackathon deployment: registered a test name via the ETH Registrar (commit-reveal, paid in MockUSDC), deployed a dedicated Permissioned Resolver, wrote/read a text record through the Universal Resolver, deployed a UserRegistry subregistry and registered a device subname under it with its own separate resolver, then delegated `ROLE_SET_TEXT` scoped to exactly one text key to a held-nothing-else test address. Confirmed on-chain: that address could write the one granted key and was rejected (`EACUnauthorizedAccountRoles`-class revert) for every other key and for a different role entirely (`ROLE_SET_ADDRESS`). **K2 does not apply — EAC restricts as documented.** Script: `scripts/gate0.3-eac-check/index.mjs`.

**Side finding, not a gate failure:** the docs' own "Deploying a Registry Proxy" example (`initialize(address rootAccount, uint256 roleBitmap)`) does not match the actually-deployed `UserRegistryImpl` bytecode on this hackathon deployment — verified source on Sepolia Etherscan shows `initialize((address,uint256)[] grants)` instead (single array, same shape as the resolver's grants but without a `calls` array). The two-arg version reverts every time. Whoever writes the real Phase 2 subregistry deployment code should use the corrected signature, not the docs page. See `11_SOURCE_NOTES.md`.

## Gate 0.4 — Hedera x402 through Blocky402 (2 hours)

One trivial paid request, end to end, transaction confirmed on HashScan.

A team shipped Hedera x402 settlement inside a 36-hour event in July, so this should pass. If it does not pass in two hours, that is a finding and the Hedera slot is at risk. Watch HTS token association: a recipient that has not associated with the token, with no free auto-association slot, cannot receive it.

**✅ VERIFIED Sept 9, 2026** — `scripts/gate0.4-blocky402-check/`: a Hono route (`GET /paid-ping`, `@x402/hono`) priced at 0.01 HBAR, verified/settled through Blocky402's hosted testnet facilitator (`https://api.testnet.blocky402.com`, confirmed live via its `/supported` endpoint listing `hedera:testnet`) — not a local or Scaffold-HBAR-bundled facilitator, which sidesteps `11_SOURCE_NOTES.md`'s open "is the starter kit's facilitator actually Blocky402?" question entirely. A real client (`@x402/fetch`, `@x402/hedera`'s `ExactHederaScheme`, a genuine ECDSA testnet account) called it once: got a real HTTP 402, signed a real Hedera transfer, retried, got HTTP 200. Settlement transaction `0.0.7162784@1788938479.675435198` confirmed independently against the public mirror node (not just the client's own report) — a `CRYPTOTRANSFER`, result `SUCCESS`, moving 1,000,000 tinybar (0.01 HBAR) from the client account (`0.0.10433667`) to the server account (`0.0.10433704`). HashScan: https://hashscan.io/testnet/transaction/0.0.7162784@1788938479.675435198 — per Luke Forrest's own advice (`docs/sources/hedera_workshop_clean.md`), this on-chain transaction is intentional evidence for judging, not incidental; call it out the same way in the final README. Settled in native HBAR (`0.0.0`), not an HTS token, deliberately avoiding the `TOKEN_NOT_ASSOCIATED_TO_ACCOUNT` trap for this standalone proof — full writeup, findings, and a runnable repro in `scripts/gate0.4-blocky402-check/README.md`. Wall-clock: about 2 hours including package-API discovery (the `@x402/core`/`@x402/hedera` v2 surface wasn't previously verified against real `.d.ts` files) and an account-funding retry. This closes Phase 4 (`05_BUILD_PLAN.md`) Section A; Section B (metering the rendezvous relay itself) is next.

## Gate 0.5 — Name (10 minutes)

Confirm the chosen name is free on npm and GitHub. Use it consistently from the first commit.

## Reporting

After Gates 0.1–0.4, report: pass/fail each, wall-clock time, and anything that was harder than expected. If 0.2 fails, report immediately rather than continuing to 0.3.
