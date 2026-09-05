# Tech Stack

Verified Sept 4, 2026 where marked. Everything else: check on Day 0 and pin.

| Layer | Choice | Notes |
|---|---|---|
| Node agent language | **Go** | wireguard-go and pion/ice are both Go. Also the right call for a CLI daemon. See `adr/0002-node-agent-language.md` |
| WireGuard | `wireguard-go` (userspace) | Kernel WireGuard is faster but userspace is portable and demoable on a laptop. Do not implement the protocol |
| NAT traversal | `pion/ice` or `pion/stun` | STUN + hole punching. Public STUN available (e.g. `stun.l.google.com:19302`) |
| Rendezvous relay | Custom Go service | Candidate exchange only, x402-metered, used on every connection attempt. See `adr/0003-rendezvous-relay-split.md` |
| Data relay | Custom Go service | Forwards opaque UDP, x402-gated per byte, used only if hole punching fails. See `adr/0003-rendezvous-relay-split.md` |
| Naming / admission | **ENSv2 on Sepolia** | Beta since ~mid-Aug 2026. Permissioned Registry, Permissioned Resolver, Enhanced Access Control |
| ENS client | `viem` directly, not `ensjs` | Gate 0.3 (the only thing proven against this deployment's custom addresses and the `UserRegistryImpl.initialize` signature drift) used raw `viem`; Phase 1 Section A implemented the sidecar the same way. `ensjs` on top remains possible future work, not required. Go never calls contracts directly either way — decided, not discretionary. See `adr/0002-node-agent-language.md` |
| ENS sidecar | **Hono on Bun**, one per node, local HTTP only | Wraps `viem`; the Go agent's only path to ENS state. Per-node, not shared — see `adr/0002-node-agent-language.md`. Runs on Bun (not plain Node) so TypeScript executes directly, no `tsx`/`ts-node` transpile step |
| TS package manager | **pnpm**, everywhere | Sidecar, provisioning/dev scripts, and the admin CLI all use `pnpm install` / committed `pnpm-lock.yaml` |
| ENS agent records | ENSIP-25, ENSIP-26 | Use standard record keys, do not invent |
| Hardware signer | `@ledgerhq/wallet-cli` **v1.0.1** (verified) | Install `npm i -g @ledgerhq/wallet-cli`. **Marked v1 experimental; flags and behavior may change** |
| Ledger key encryption | `wallet-cli ring` (`init/encrypt/decrypt/keys/destroy`) | LKRP-backed **encryption**, not signing. See `adr/0001-ledger-ring-vs-send-split.md` |
| Ledger agent skill | `npx skills add LedgerHQ/agent-skills -s wallet-cli-usage` (verified) | Give this to the coding agent |
| Ledger DMK skills | `npx skills add ledgerhq/agent-skills` | For build-time integration guidance |
| Payments | `@x402/core`, `@x402/hedera` (v2 scope) | v1 packages (`x402-express` etc.) are deprecated |
| Facilitator | **Blocky402** | Required by the Hedera track |
| Chain (payments) | Hedera testnet | Watch HTS token association — no EVM analogue |
| Admin CLI / demo UI | Node + TypeScript, optionally Next.js 16 | Keep it minimal; the demo is terminals, not a dashboard |
| Tests | Go stdlib testing + Vitest for TS parts | |

## Two-chain note

ENS is on Sepolia, payments on Hedera. This is forced by the tracks and is defensible here in a way it was not for earlier ideas: **identity and payment are genuinely separate concerns.** Say it that way. Authorization lives where naming lives; settlement lives where cheap metered payments live. Do not pretend it is one system.

## Hackathon ENSv2 Sepolia deployment (confirmed Sept 4, Kevin | ENS Labs, Discord)

A dedicated ENSv2 deployment is live on Sepolia for ETHOnline 2026, separate from the production/beta deployment. **Build against this one, not the addresses on the production docs.**

| What | Link |
|---|---|
| Deployment addresses + ready-made viem/ethers snippets | https://feature-permres-inode-refact.docs-bao.pages.dev/learn/deployments#sepolia-ensv2-beta |
| ENSv2 docs for this deployment | https://feature-permres-inode-refact.docs-bao.pages.dev/ensv2/overview |
| ENS Explorer (hackathon deployment only) | https://hackathon-deployment-portal-app.ens-cf.workers.dev/ |
| ENS App — register hackathon names here | https://hackathon-deployment-manager-app-v4.ens-cf.workers.dev/ |

**⚠️ Universal Resolver gotcha (ENS Labs' own wording: "important").** viem and ethers both ship a built-in Universal Resolver address. It must be overwritten once in code with the hackathon's Universal Resolver address, or every resolution call silently targets the production deployment instead — a wrong Universal Resolver address looks exactly like a working call that happens to resolve against the wrong contracts. Ready-made override snippets are on the deployments page above. Do this before Gate 0.3.

## Unknowns to resolve Day 0

1. Does a physical Ledger device exist to test with? (`11_DAY0_GATES.md`, Gate 0.1)
2. Can two peers on different networks connect with only STUN plus a dumb relay? (Gate 0.2)
3. ~~ENSv2 Sepolia contract addresses~~ — resolved, see above. Whether EAC role delegation works as documented is now also resolved — see Gate 0.3 in `11_DAY0_GATES.md`.
4. `@x402/hedera` + Blocky402: one paid request end to end. Blocky402 is confirmed required by the official track page; still open whether Scaffold-HBAR's bundled x402 starter facilitator *is* Blocky402 or needs swapping — see `12_SOURCE_NOTES.md`.
5. Whether `wallet-cli ring init` can be run once on a laptop and the resulting ring used on a headless VPS without the device present. **This is the entire premise of the Ledger enrollment story.**
