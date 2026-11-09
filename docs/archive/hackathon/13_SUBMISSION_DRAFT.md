# ETHGlobal Mumbai 2026 submission draft

Form copy for the continuity entry. Fill every bracket after the final run. Delete any claim marked **Open** that stays Open. This file replaces the earlier ETHOnline draft, which targeted Ledger. Ledger is out of scope for Mumbai.

Pair it with the [demo checklist](DEMO_CHECKLIST.md) and the [pre-existing vs new section](../README.md#pre-existing-vs-new) of the README.

## Project

**Name:** Bramble

**Tagline:** Private network access for agents, with permissions enforced by every peer.

**Track:** Continuity (existing project). Baseline: tag `pre-mumbai` (commit `28e2a8f`). List new work with `git log pre-mumbai..HEAD`.

**Short description:** Bramble is a WireGuard mesh whose device identities and scoped service grants live in an ENSv2 registry. Every peer resolves that registry itself, so no coordinator and no relay can add a member or keep a revoked one. Workers reach a private service without holding its credential. A human approves each grant and can revoke it live. Public relays are paid per session or byte allotment through x402 on Hedera. For Mumbai we moved the control plane onto Tor and added an audit trail on Hedera Consensus Service (HCS).

**Repository:** https://github.com/WhyAsh5114/bramble

**Demo video:** [2–4 minute video URL]

**Live deployment:** [public console URL, or "recorded demo on two cloud machines"]

**Partner prizes selected:** ENS, Hedera, Ethereum Foundation. Also choose "Finalist and Partner Prizes".

## What existed before Mumbai

At `pre-mumbai` Bramble already had: userspace WireGuard nodes in Go (`brambled/`), the ENS and x402 sidecars (`sidecar/`), a relay and its payment sidecar (`relay/`, `relay-sidecar/`), an admin CLI (`admincli/`), and a read-only dashboard (`dashboard/`). Sepolia ENS authorization and Hedera testnet x402 relay payments worked. We did not count polish, bug fixes or documentation of that behavior as new work.

## What is new since `pre-mumbai`

| Area | New work | Status to state in the form |
| --- | --- | --- |
| Approval console (W1, W4) | Access requests, approve and revoke, device join, activity feed | [fill from README table] |
| Agent access (W2, W3) | MCP server for requesting access; per-agent spend with a HashScan link | [fill from README table] |
| Tor control plane (W9) | Sidecar ENS reads and payment HTTP go through Tor with separate isolation keys | ENS and payment fetches routed; Tor-exit check and revocation-over-Tor measurement recorded 6 Nov; before/after provider-log comparison **Open** |
| Hedera HCS audit trail (W10) | Salted commitment per grant, revoke and settled payment, posted by the approval service | [PR #22](https://github.com/WhyAsh5114/bramble/pull/22); a fresh mirror-verified record from the cloud deployment **Open** |
| Direct paths (W8) | STUN discovery, hole punching, relay fallback | **Open** unless rehearsed on camera |
| Distribution (W5, ETH-177) | Self-contained device binaries, `bramble up`, local web UI, sponsored relay sessions | [fill from README table] |
| Demo infrastructure (W11) | Cloud nodes, logging RPC proxy, public console, preflight | [fill from README table] |

## ENS: ENSv2 in an existing project

Bramble uses an ENSv2 Permissioned Registry as the network's authorization source. The demo tailnet is `bramble-mumbai-20261107.eth` on Sepolia. Each device is a subname with its own Permissioned Resolver. Enhanced Access Control gives separate accounts narrow rights for enrollment, key rotation, revocation, access rules and relay registration. Access records hold digests of service names, never plaintext. Each peer re-resolves the registry and updates its own WireGuard peer table, so a changed record changes live access without restarting anything. Relay endpoints sit in a separate registry, so device records never publish topology.

**New for Mumbai:** the node sidecar reads ENS through Tor (`torFetch('ens')`), with CCIP-Read disabled so no off-chain lookup bypasses that transport. The RPC provider no longer sees node IP addresses. It still sees names, requested records and timing.

**Evidence to link:** `sidecar/src/ens/client.ts`, `admincli/src/`, `brambled/admission/`, the console's Sepolia approve and revoke receipts, and the video timestamps for beats 1, 2 and 5. Reported by ETH-244 on 6 Nov (build `4928612`): first failed call 36.6 s and 37.0 s after revoke began.

## Hedera: Built on Hedera (Continuity Track)

**Existing:** x402 relay payments settle in testnet USDC through the Blocky402 facilitator. A node pays for a rendezvous token or an explicit data-relay byte allotment, and the client enforces asset, network and per-payment limits before it signs.

**New Hedera service:** an HCS audit trail. For each grant, revoke and settled payment, the approval service posts a salted hash commitment to an HCS topic. The console checks a retained opening against the mirror node and links the entry on HashScan. Only commitments are public. Kind and timing remain visible, and the event and salt stay off-chain. Format: [`docs/16_HCS_AUDIT_RECORD_FORMAT.md`](16_HCS_AUDIT_RECORD_FORMAT.md). HCS does not enforce access and does not prove that every event was logged.

**Evidence to link:** `hedera-audit/`, the console's audit panel, the HashScan topic entry for the demo run **[Open until recorded]**, and a HashScan payment from the same run. Payment settlement and HCS commitment are separate records; show both, and never present one as the other.

## Ethereum Foundation: Best CROPS Upgrade to an Existing Project

Before Mumbai, every node sent its ENS polls and payment requests straight to a public RPC endpoint and a payment facilitator, which could link a node's IP address to its network and its payments. Now the sidecar sends both through Tor with separate isolation keys (`ens`, `pay`). The protected requests fail closed until Tor is ready. Authorization still comes from ENS and is enforced by each peer; Tor changes only who can observe the reads.

CROPS, with the dependencies we disclose ([README](../README.md#crops)):

- **Censorship resistance:** no coordinator grants or blocks access. We still depend on an RPC endpoint, relays and an administrator.
- **Open source:** [licence and repository link].
- **Privacy:** ENS reads and payment HTTP go over Tor. WireGuard, STUN and relay UDP stay outside Tor, so relays and STUN servers see peer IP addresses. The RPC provider sees names and timing. Payments expose the payer account.
- **Security:** peers check access themselves; the agent never holds the backend credential. The admin and payer use software keys.

**Evidence to link:** [Tor design](adr/0013-tor-transport.md); the [6 Nov measurement](measurements/2026-11-06-revocation-over-tor.md) (two 600 s runs on one machine; replayed median detection 7.0–11.5 s, worst observation 29 s, excluding chain confirmation); the Tor-exit check recorded 6 Nov; the provider-log before/after clip **[Open until recorded]**.

## Technical summary

- Go userspace WireGuard nodes and relay.
- TypeScript/Bun ENSv2, x402 and Tor-routed sidecars.
- ENSv2 on Sepolia: Permissioned Registries, Permissioned Resolvers, Enhanced Access Control.
- Hedera testnet: USDC settlement through Blocky402, and HCS audit commitments.
- Approval console and MCP server for human-approved agent access.
- Next.js read-only dashboard.

## Known limitations

State these in the form; they are true today.

- Tor-backed decisions take time. Measured 7 Nov on the live cloud setup: about 25 s per gateway decision, 24 s to deny and 35–41 s to succeed. A fresh device reached its first task result 204 s after approval began (one run). Revoke to first denied call took 33 s (worker) and 42 s (fresh device). We have not isolated Tor's share. Batching the reads (`core-sol/eth-267-fast-resolve`, unmerged and unmeasured on the cloud pair) is the next step.
- Revocation is not instant. It depends on the ENS poll interval, RPC behavior and Tor. Confirmation and first failed call are separate times.
- Tor covers the control plane only. Data-plane UDP, STUN and relays expose addresses to their counterparties.
- Node, admin and Hedera payer keys are software keys. Relay admission is capped but not production-hardened.
- The demo shows a worker running as a Unix user, not a proven autonomous agent session, unless a separate rehearsal shows one. The demo token also permits approve and revoke, so the deployed agent could self-approve; keep that authorization with the human.
- Items marked **Open** above have no recorded evidence at the time of writing.

## Final checklist

- [ ] Clean checkout installs and `pnpm verify` passes.
- [ ] README "Pre-existing vs new" table is current; every row beyond `planned` links its PR or commit.
- [ ] No secrets, authenticated RPC URLs, private IPs or private working notes committed.
- [ ] Beats 1, 2, 3 and 5 recorded from current chain state, with a real Sepolia receipt and a real HashScan payment.
- [ ] HCS record shown only if a fresh mirror-verified receipt exists; otherwise the form says it is not configured in the recorded run.
- [ ] Beat 4 and beat 6 included only if recorded; otherwise removed from the form copy.
- [ ] Video 2–4 minutes, at least 720p, human narrated, not sped up.
- [ ] Partner selections: ENS, Hedera, Ethereum Foundation. "Finalist and Partner Prizes" chosen.
