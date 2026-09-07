# Track Fit

Three partner slots. A multi-track partner counts as one.

## ENS — Best Use of ENSv2

Tightest fit on the board. Their ask, mapped:

| Their words | Your build |
|---|---|
| "deploy your own subname registry to tokenize and manage subnames under your own rules" | The tailnet is a registry. Devices are subnames under it |
| "Give subnames their own Permissioned Resolver so they fully own their data" | Each device's resolver holds its own WireGuard public key. A device can rotate its own key and nothing else |
| "Use Enhanced Access Control... to delegate specific rights — like letting an account edit only certain text records" | A team lead may enroll devices but not change ACLs. A device may rotate its key but not enroll others |
| "expiring, revocable, non-transferable vs. transferable" | Contractor devices expire. Laptops are non-transferable. Revocation is the demo |
| "Bonus points if you bring AI agents into the mix — think agents as namespaces, each with their own identity and permissions" | `agent-1.acme.eth` gets a node key, one ACL rule, and an expiry |

**Hard requirements:** built on ENSv2 Sepolia; ENSv2 features central, not cosmetic; **demo must be functional and not hard-coded**; video or live demo; open source.

**Also in their resources, worth using:** ENSIP-25 (AI agent registry name verification) and ENSIP-26 (agent text records). Using the standard record keys instead of inventing your own is close to free and signals you read their docs. See `docs/10_JUDGING.md` objection 13 for how ENSIP-26 applies to relay subnames but not device/agent subnames.

## Ledger — AI Agents x Ledger

Three of their four wanted directions land:

| Their words | Your build |
|---|---|
| "Bring the Key Ring to hosts with no USB port: enroll a VPS, a CI runner, or a hosted agent" | Exactly the enrollment flow. The VPS holds its WireGuard private key encrypted under the Key Ring |
| "Agents that use secrets they cannot leak: a broker hands out scoped capabilities, never the API key" | The agent gets a network position, not a credential. Its node key is worthless unless the registry authorizes it |
| "Human-in-the-loop agents where Ledger approves high-risk actions before funds move or permissions escalate" | Enrolling or revoking a device is a permission escalation, confirmed on the device |
| "Agents that pay for what they use... including x402-style patterns" | Relay bandwidth payment |

**Critical constraint, verified:** the first two bullets "must be built on the Ledger Agent Stack, and in particular on the Ledger Key Ring CLI (`wallet-cli ring`)."

**What `ring` actually is** (verified Sept 4): `ring init / encrypt / decrypt / keys / destroy`, LKRP-backed **encryption** of files and text. It is not a general signing tool. See `docs/adr/0001-ledger-ring-vs-send-split.md` for the full split and its LKRP rotation limitation.

## Hedera — AI & Agentic Payments

| Their words | Your build |
|---|---|
| "Metered data feed. Price by query, settle per request, **no seats or subscriptions**" | The seat-pricing argument, in their own words. Relay bandwidth metered per byte |
| "Micropayment streaming. Settle every few seconds for compute or bandwidth in use" | Relay billing |
| "Host a live x402-gated service on Hedera testnet or mainnet, settled through the **Blocky402** facilitator" | The relay is the x402-gated service |
| "Build a platform or agent that consumes that service and completes at least one real paid request end to end" | The agent node pays for relay bandwidth |

**Extra points, full list from the official prizes page (Sept 7):** **metering rather than a flat per-request charge** (pay-per-call/data/compute — the plan already does this, Gate 4.2); on-chain agent identity via **ERC-8004 or HCS-14**; agent discovery via **UCP, or a directory that makes the service findable** (the ENS relay directory counts); verifiable **payment audit trails on HCS**; **HTS tokens or custom fee schedules in the settlement path**; **recurring or streamed payments using Scheduled Transactions** (a natural fit for periodic relay-usage settlement — batch-settle accumulated usage via a scheduled transaction); multi-agent negotiation via **A2A or ACP** (least relevant — no second agent in scope).
