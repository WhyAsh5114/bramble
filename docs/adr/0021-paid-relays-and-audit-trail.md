# ADR 0021 — Paid relays and an HCS audit trail on Hedera, both opt-in

**Kind:** Explanation. **Status:** Proposed, 10 Nov 2026. The maintainer chose Hedera as the payment rail and decided both features are opt-in. Nothing here is built. Amends [ADR 0019](0019-user-driven-tailnets.md), which retired x402 metering and the HCS audit trail from the main tree. Evidence labels: **Read from source** (read the code or docs, did not run it) and **Open** (undecided or untested).

## Context

ADR 0019 removed Hedera payments and the Hedera Consensus Service (HCS) audit trail because both depended on a hosted approval service and an operator who paid for everything. That reason no longer applies to the features themselves. Two needs remain:

- **Relay cost.** The project relay stops forwarding when its byte budget runs out (`relay/relaysrv/budget.go`, **Read from source**). A third party can run a relay and list it, but has no way to be paid for it.
- **Audit.** Grants and revokes are public ENS records. Relay usage and gateway access decisions are not recorded anywhere shared. A gateway's allow and deny decisions live only in that node's local activity log.

The old designs are in the `archive/hackathon` tag: [ADR 0007](0007-rendezvous-relay-metering.md) and [ADR 0008](0008-data-relay-metering.md) for x402 payments, and [ADR 0014](0014-hcs-audit-trail.md) for HCS. Those runs settled real Hedera testnet payments that the mirror node confirmed (**Read from source**, ADR 0008).

## Decision

1. **Hedera is the payment rail and the audit chain.** Relay payments are small and frequent, and the maintainer expects Hedera fees to suit pay-per-byte pricing better than Ethereum. **Open:** confirm current Hedera transfer fees and USDC availability on the network used. On Sepolia the gas is free, so the cost gap shows only on mainnet. That comparison is reasoning, not a measurement.
2. **Both features are opt-in.** A node, tailnet or relay that does not opt in needs no Hedera account, and the free relay path works with none. Defaults stay as they are today.
3. **Payment and audit never gate admission.** Peers still enforce access from ENS state. A failed payment check stops a paid relay forwarding for that payer. A failed audit post changes nothing (ADR 0014, decision 5).
4. **Hedera testnet only until the maintainer decides otherwise.** `AGENTS.md` forbids mainnet writes and real money.

## Constraint change from ADR 0019

ADR 0019 kept chain keys out of the node. This proposal gives an opt-in node a **Hedera key** for payments and, if enabled, for audit posts.

- The key is separate from the tailnet wallet. It cannot write tailnet records or grant access.
- The node generates it on opt-in and stores it in the state directory with mode `0600`. The user funds its account from a testnet faucet. The UI shows the account ID and balance, never the key.
- A node that does not opt in holds no Hedera key.

`AGENTS.md` says "never add a server-side component that holds chain keys". The node is user-side, and the key is the user's own, but the wording needs a maintainer decision (D3).

## Paid relays

### Prepaid bundles over a direct transfer

A paying node buys a bundle of bytes with one Hedera token transfer. No contract is needed.

1. A relay operator publishes two fields in the relay's registry record: `price` (atomic USDC per MiB) and `hedera-account` (the account that receives payment). **Open:** field names and where they sit in the record schema.
2. The node sends a USDC transfer to that account. The memo carries a unique payment ID and the node's public key.
3. The relay finds the transfer through a mirror node, checks the amount against the price, and credits that public key with the bytes.
4. The relay counts bytes it forwards for that key and stops at zero. It keeps introducing peers, as the free relay does when its budget is spent.
5. The node buys a new bundle before the old one runs out, or after the relay says it is spent.

A bundle is small and a transfer is cheap, so the most a node can lose to a dishonest relay is one bundle. The relay can still take payment and not forward. A client that sees this stops buying from that relay. **Open:** a client-side byte count to compare, and whether a refund path is worth building.

The relay only receives payment. It needs a mirror-node reader but no signing key. It holds a key only if it also posts audit receipts.

### What changes

- Relay (Go): a mirror-node check, a per-key byte balance, and enforcement behind a flag. **Open:** keep the balance across restarts in the state volume so a restart does not erase paid bytes.
- Registry: `price` and `hedera-account` on the relay record.
- Node: a Hedera client that signs one transfer. The product is one Go binary, and the archived payment client was TypeScript. **Open:** whether the Hedera Go SDK covers this. I believe an SDK exists and have not checked it. This is the largest implementation risk, and the answer decides whether the "one binary" rule holds.
- UI: an opt-in switch, the Hedera account ID and balance, and a balance per relay.
- The project's free relay is unchanged.

### Relay selection

`chooseRelays` already picks from the directory (ADR 0020). A paying node would prefer a free relay while it has budget, then the cheapest paid relay. **Open:** exact ordering.

### Alternatives considered

| Option | Why not the default |
|---|---|
| Prepaid credit in an Ethereum contract | Needs a contract, and every deposit costs Ethereum gas. Hedera transfers are simpler. |
| x402 per request, as in the archive | The node signs every request, and it needed a TypeScript payment sidecar on both the node and the relay. A prepaid bundle needs far fewer signatures and no sidecar. |
| Payment channels | Correct, but far more work than this relay needs. Revisit if bundles prove too trusting. |

### Trust in the mirror node

The relay believes the mirror node's answer. A public mirror node is a third party, and a lying or lagging one can delay or deny credit. **Open:** run against more than one mirror, and the delay between a transfer and its mirror record.

## HCS audit trail

### What to record

Grants and revokes are already public in ENS records, so this ADR does not post them. Two kinds of event have no shared record today:

1. **Relay usage receipts.** A paid relay posts a commitment for each settled bundle: payer key, bytes, price. The payment is on Hedera, so the receipt sits next to it.
2. **Gateway access decisions.** A tailnet that opts in posts each allow and deny for a service.

The record format stays as merged for ADR 0014: `{"v":1,"kind":…,"c":…}`, where `c` is an HMAC-SHA256 commitment keyed by a fresh per-entry salt. Never post a plaintext service name, host or port. The holder of the openings reveals one to a verifier. **Read from source:** `hedera-audit/` and `docs/16_HCS_AUDIT_RECORD_FORMAT.md` in the archive tag.

### Who posts

- **A relay operator** who opts in posts receipts to a topic they own, with their own Hedera key.
- **A tailnet** that opts in creates a topic and records its ID in a tailnet record. Each gateway posts to it with its node's Hedera key. **Open:** the tailnet record name, and who may read the openings. The old design held them in the approval service, which no longer exists.

A failed post never blocks traffic or changes an access decision.

### What a mirror match proves

Carried over from ADR 0014. A match shows the mirror node returned bytes whose commitment the opening reproduces. It does not prove the event happened, and it does not prove the log is complete, because the poster can omit events.

## Decisions

| ID | Question | Status |
|---|---|---|
| D1 | Payment rail | **Decided:** Hedera, testnet, opt-in. |
| D2 | May an opt-in node hold a Hedera key? | **Decided:** yes, a separate key that cannot touch tailnet records. |
| D3 | Amend the `AGENTS.md` key rule to allow D2 and an operator's own key on their own relay? | **Open:** maintainer. |
| D4 | Mainnet or real money for Hedera payments? | **Open:** testnet only for now. |

## Order of work

1. Check that a Go client can sign a Hedera token transfer. If it cannot, decide between a TypeScript helper and dropping the one-binary rule. This comes first because it can change the design.
2. Settle D3 and mark this ADR Decided.
3. Add `price` and `hedera-account` to the relay record. Test them in the registry cache.
4. Relay: mirror-node verification, byte balance and enforcement behind a flag. Test that forwarding stops at zero credit and that peers still connect directly.
5. Node and UI: opt-in switch, key generation, funding view and bundle purchase.
6. HCS: relay receipts, then gateway access decisions.

Each step is tested before the next starts. This ADR describes none of it as working.

## Consequences

- Third parties can run a relay and be paid, so the single subsidised relay is no longer the only option.
- An opting-in node holds a Hedera key and needs a funded Hedera account. A node that does not opt in is unchanged.
- The relay depends on a mirror node for payment checks.
- Audit records exist only for events a poster chooses to post. They are not a complete log.
