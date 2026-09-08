# Day 0 Gates

**Do these before writing any product code.** Two of them can kill the project. Report results before proceeding.

## Gate 0.1 — Is there a physical Ledger device? (15 minutes)

The Ledger track requires the Agent Stack, and specifically `wallet-cli ring`. `ring init` requires a Ledger device over USB. Sub-check: confirm that a ring initialised on a laptop can be used on a headless host **without the device attached**, because "bring the Key Ring to hosts with no USB port" is the whole enrollment story.

- **No device available** → the Ledger slot is dead. Do not attempt it. **Report immediately**; this changes `02_TRACK_FIT.md`.
- **Device available but the headless flow does not work** → keep Ledger only for the `wallet-cli send` device-confirmation path (enroll/revoke signing), and drop the Key Ring claims entirely.

**Test:** `wallet-cli --version` returns v1.0.1 or later; `wallet-cli ring init` completes; `ring encrypt` on the laptop and `ring decrypt` on a second machine with no device attached.

**Status:** design confirmed, physical run still pending as of Sept 7. **This is a Sept 7–8 item:** Phase 3 (Sept 9–10) cannot start without the actual physical run, still waiting on device access. The Ledger "Tracks Explained" workshop (watched Sept 7 — transcript + slides in `docs/sources/ledger_workshop_clean.md`) removed the risk that headless `ring` decryption doesn't work as hoped: Ledger's own speakers named "a VPS, CI runner, anything hosted that doesn't have direct access to your device" as the *explicit intended use case* for Key Ring, confirmed it live in a demo, and walked through the mechanism in Q&A. See `12_SOURCE_NOTES.md`'s "✅ RESOLVED" entry. **Still open:** the `wallet-cli send` calldata question (not addressed by the workshop) and the actual hands-on `ring init`/`encrypt`/`decrypt` run once the device is on hand — this finding de-risks the premise, it doesn't substitute for running the test. **`wallet-cli send --help` checked directly (Sept 7): resolved** — `--data` accepts 0x-prefixed EVM calldata, not transfer-only. See `adr/0001-ledger-ring-vs-send-split.md`.

**Device note:** run this gate against a dedicated device, not hardware holding real funds — `ring` is new/beta functionality, and a wallet-level reset or passphrase profile doesn't rule out a bug below the seed layer (firmware, secure-element state). A Nano S Plus is sufficient (no touchscreen/Transaction Check needed for `ring`/`send` against Sepolia). One is being acquired; this gate is blocked on it arriving.

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

**Side finding, not a gate failure:** the docs' own "Deploying a Registry Proxy" example (`initialize(address rootAccount, uint256 roleBitmap)`) does not match the actually-deployed `UserRegistryImpl` bytecode on this hackathon deployment — verified source on Sepolia Etherscan shows `initialize((address,uint256)[] grants)` instead (single array, same shape as the resolver's grants but without a `calls` array). The two-arg version reverts every time. Whoever writes the real Phase 2 subregistry deployment code should use the corrected signature, not the docs page. See `12_SOURCE_NOTES.md`.

## Gate 0.4 — Hedera x402 through Blocky402 (2 hours)

One trivial paid request, end to end, transaction confirmed on HashScan.

A team shipped Hedera x402 settlement inside a 36-hour event in July, so this should pass. If it does not pass in two hours, that is a finding and the Hedera slot is at risk. Watch HTS token association: a recipient that has not associated with the token, with no free auto-association slot, cannot receive it.

**Status:** not started as of Sept 7. **This is now urgent, not "Day 0":** submissions close Sept 13 16:00 UTC / 21:30 IST (`09_EVENT_RULES.md`), and Phase 4 (Sept 10–11) is built entirely on this gate. Run it Sept 7–8. See `12_SOURCE_NOTES.md` for the open question on whether Scaffold-HBAR's bundled facilitator is actually Blocky402.

## Gate 0.5 — Name (10 minutes)

Confirm the chosen name is free on npm and GitHub. Use it consistently from the first commit.

## Reporting

After Gates 0.1–0.4, report: pass/fail each, wall-clock time, and anything that was harder than expected. If 0.2 fails, report immediately rather than continuing to 0.3.
