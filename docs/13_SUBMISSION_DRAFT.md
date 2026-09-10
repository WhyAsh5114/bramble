# ETHOnline 2026 submission draft

Use this as form copy after the final live run. Replace every bracketed item and remove any feature that is not working in the recorded build.

## Project

**Name:** Bramble

**Tagline:** Private network access for agents, with permissions enforced by every peer.

**Short description:** Bramble is a WireGuard mesh whose device identities and scoped service grants live in an ENSv2 registry. Peers resolve authorization independently, so a rendezvous or data relay cannot add a member. Agents reach private services without receiving the backend credential; a human can approve broader access on Ledger, while x402 pays independent relays per session or byte allotment on Hedera.

**Repository:** [public GitHub URL]

**Demo video:** [2–4 minute video URL]

**Live deployment/demo:** [URL or “recorded two-machine demo”]

## ENS — Best Use of ENSv2

Bramble uses an ENSv2 Permissioned Registry as the network's authorization source. Each device is a subname with its own Permissioned Resolver. Enhanced Access Control gives separate accounts narrowly scoped enrollment, key-rotation, revocation, ACL, and relay-registration rights. Each peer re-resolves the registry and updates its own WireGuard peer table; changing chain state changes live network access without restarting a coordinator. Relay endpoints are isolated in a separate registry so device records never publish network topology.

**Evidence to link:** `sidecar/src/ens/client.ts`, `admincli/src/`, `brambled/admission/`, Gate 2.1 transaction evidence, and the video timestamps for deny/grant/revoke.

## Ledger — AI Agents x Ledger

The agent starts with narrow access and its wider request is denied. The administrator's ACL-granting key is never stored as plaintext: it is encrypted at rest under a Ledger Key Ring (LKRP), rooted to a genuine Nano S Plus present at provisioning time, and only ever decrypted in memory, per grant, on that same machine — a physical Ledger cannot be substituted or emulated for that step. Widening the agent's scope, and the agent's subsequent success at the previously-denied task, both happen live, without a restart. The agent never receives the private backend's API credential.

`wallet-cli send` itself does not support Sepolia or other testnets/L2s as of v2.1.0 (confirmed against Ledger's own `agent-skills` repo, not just an error message — see `docs/12_LEDGER_DX_FEEDBACK.md` Gap 4), so the on-chain write that follows a grant is signed by a software key, not the device. [Update if `admincli/src/ledger-eth-sign.ts`'s direct hw-app-eth signer lands for `enroll.ts` before submission — that would add a real per-transaction physical confirmation to the enrollment path specifically.]

We also provide detailed developer feedback from real Nano S Plus and wallet-cli 2.1.0 testing, including the Ledger Sync app prerequisite, local password setup, and fresh-host Key Ring provisioning behavior.

**Evidence to link:** final Ledger wrapper/code, `docs/12_LEDGER_DX_FEEDBACK.md`, the on-device video timestamp, and the ENS transaction.

## Hedera — AI & Agentic Payments

Bramble runs an x402-gated relay service on Hedera testnet through the Blocky402 facilitator. A node discovers relay payment URLs through ENS, pays testnet USDC for a rendezvous token or explicit data-relay byte allotment, and receives the forwarding service without an API key or subscription. Different allotment sizes produce different prices. The client applies per-byte, per-session, asset, network, and per-payment limits before signing. The two-relay demo selects by price/latency and recovers through the backup after the primary process is killed.

**Evidence to link:** `relay-sidecar/src/`, `sidecar/src/payments/`, `brambled/live_demo.go`, transaction IDs/HashScan URLs, and the paid-request video timestamp.

## Technical summary

- Go userspace WireGuard nodes and relay.
- TypeScript/Bun ENSv2 and x402 sidecars.
- ENSv2 Sepolia Permissioned Registry, Permissioned Resolvers, and EAC.
- Hedera testnet USDC settlement through Blocky402.
- Ledger wallet-cli/Key Ring for the final human approval path.
- Next.js read-only dashboard for device, ACL, and relay state.

## Known limitations

Bramble does not yet implement STUN, automatic NAT-failure detection, automatic mesh addressing, or production relay authentication. Relay sessions are short-lived and do not handle NAT remapping. Node and Hedera payer keys are currently software keys. Revocation timing depends on the ENS polling interval and resolver availability. The demo uses explicit peer labels and mesh IP assignments.

## Final checklist

- [x] Check-in #2 completed in the Hacker Dashboard.
- [x] Repository public and license visible.
- [ ] Clean checkout installs and passes required checks.
- [ ] Root README environment and launch path verified on both demo machines.
- [ ] No secrets, private IPs, credentials, or private working notes committed.
- [ ] ENS denied/allowed actions and revocation demonstrated from current chain state.
- [ ] Ledger copy retained only if the final device-backed path works on camera.
- [ ] Hedera paid request and settlement ID visible in the video.
- [ ] Video is 2–4 minutes, at least 720p, human narrated, and not sped up.
- [ ] Partner selections: ENS, Hedera, and Ledger only if its final path is complete.
