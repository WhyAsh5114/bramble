# ADR 0021 — Paid relays and an HCS audit trail, without a hosted service

**Kind:** Explanation. **Status:** Proposed, 10 Nov 2026. Nothing here is built. Amends [ADR 0019](0019-user-driven-tailnets.md), which retired x402 metering and the HCS audit trail from the main tree. Evidence labels: **Read from source** (read the code or docs, did not run it) and **Open** (undecided or untested).

## Context

ADR 0019 removed Hedera payments and the Hedera Consensus Service (HCS) audit trail because both depended on a hosted approval service and an operator who paid for everything. That reason no longer applies to the features themselves. Two needs remain:

- **Relay cost.** The project relay stops forwarding when its byte budget runs out (`relay/relaysrv/budget.go`, **Read from source**). A third party can run a relay and list it, but has no way to be paid for it.
- **Audit.** Grants and revokes are public ENS records. Relay usage and gateway access decisions are not recorded anywhere shared. A gateway's allow and deny decisions live only in that node's local activity log.

The old designs are in the `archive/hackathon` tag: [ADR 0007](0007-rendezvous-relay-metering.md) and [ADR 0008](0008-data-relay-metering.md) for x402 payments, and [ADR 0014](0014-hcs-audit-trail.md) for HCS. **Read from source.**

## Constraints from the rebuild

1. **The node holds no chain key.** Tailnet writes are signed by the user's wallet in the browser (ADR 0019). The old x402 flow needed a key on the paying node to sign every purchase, so it cannot return as it was.
2. **No hosted service run by the project.** A paid relay is run by its own operator, not by the project.
3. **Peers still enforce access from ENS state.** Payment and audit never gate admission (ADR 0014, decision 5: audit failure is non-blocking).
4. `AGENTS.md` forbids "a server-side component that holds chain keys or sponsors gas". A relay operator who holds their own receiving or posting key may conflict with that wording. **Open:** the maintainer decides whether to amend it (decision D4).

## Proposal: paid relays

### Prepaid credit instead of a payment per request

A member tops up credit once, from the browser wallet. The relay spends it as it forwards. The node signs nothing.

1. A relay operator publishes a `price` record (atomic USDC units per MiB) and the credit contract address in the relay's record in the relay registry.
2. A tailnet member deposits MockUSDC into the credit contract for that relay. One wallet signature, in the browser, like every other write.
3. The relay reads the deposit from the chain, then counts bytes it forwards for that payer and subtracts them from the balance in memory.
4. When the balance reaches zero the relay stops forwarding for that payer and keeps introducing peers, the same behavior as the free relay when its budget is spent.
5. The operator claims earnings against a usage statement. A deposit unlocks for withdrawal after a delay, so the operator is not paid for a refund that raced the usage.

The relay can overcharge, because usage is counted off chain. Three things limit the harm: deposits are small, each relay has its own balance, and a client that sees a relay misbehave can stop topping up. **Open:** a dispute mechanism, and whether the client should count the bytes it sends and compare.

### Alternatives considered

| Option | Why not the default |
|---|---|
| Keep x402 per request on Hedera USDC | The node must hold a Hedera key and sign every purchase. This breaks constraint 1 and needs a second wallet and a second chain. |
| Payment channels | Correct, but far more contract and client work than a portfolio-scale relay needs. Revisit if prepaid credit proves too trusting. |
| Subscription to a relay | Needs an identity the relay can bill, which duplicates what the credit contract already gives. |

### Relay selection

The node already picks relays from the directory (`chooseRelays`, ADR 0020). It would prefer a free relay while that relay has budget, then the cheapest paid relay the payer has credit with. **Open:** exact ordering, and how a node learns that its credit is spent before a session fails.

### What changes

- Relay: a credit check and byte counter in the data relay, behind a flag. The relay stays a single Go binary, with no TypeScript payment sidecar (the old `relay-sidecar`).
- Registry: `price` and credit contract fields on the relay record.
- Contract: one credit contract (deposit, spend claim, delayed withdrawal). **Open:** one shared contract or one per relay.
- UI: a "top up relay credit" action and a balance view.
- The project's free relay is unchanged.

### Which chain for payments

Sepolia and MockUSDC, which the project already uses for the ENS registrar commit-reveal. One wallet, one chain, no new account. Hedera USDC stays possible later. **Open:** decision D1.

## Proposal: HCS audit trail

### What to record

Grants and revokes are already public and permanent in ENS records. Posting them to HCS again adds a consensus timestamp and little else. HCS earns its place for events that have no shared record today:

1. **Relay usage receipts.** A paid relay posts a commitment for each settled session: payer, bytes, price.
2. **Gateway access decisions** (opt-in per tailnet). A gateway posts allow and deny decisions for a service.

The record format stays as merged for ADR 0014: `{"v":1,"kind":…,"c":…}`, where `c` is an HMAC-SHA256 commitment keyed by a fresh per-entry salt. Never post a plaintext service name, host or port. The holder of the openings reveals one to a verifier. **Read from source:** `hedera-audit/` and `docs/16_HCS_AUDIT_RECORD_FORMAT.md` in the archive tag.

### Who posts

There is no approval service, so each poster owns a Hedera key and pays the small HBAR fee on Hedera testnet.

- **Relay operators** post usage receipts to a topic they own.
- **A tailnet that wants access logging** creates a topic and records its ID in a tailnet record. Gateways post to it with a key the member provides. **Open:** this puts a Hedera key on a node (decision D2), which is a lower-value key than a wallet but still a key.

A failed post never blocks traffic or changes an access decision. This keeps ADR 0014's rule.

### What a mirror match does and does not prove

Carried over from ADR 0014. A match shows the mirror node returned bytes whose commitment the opening reproduces. It does not prove the event happened, and it does not prove the log is complete, because the poster can omit events.

### Implementation language

The archived `hedera-audit` package is TypeScript. The rest of the product is Go plus the browser UI. **Open:** whether a Go client for HCS exists and is good enough, or whether the poster is a small Node tool run beside the relay or node. Not checked.

## Decisions for the maintainer

| ID | Question | Recommendation |
|---|---|---|
| D1 | Payment rail: Sepolia MockUSDC, or Hedera USDC? | Sepolia. One wallet and no key on the node. |
| D2 | May a node hold a low-value Hedera key to post access decisions? | Yes, opt-in and clearly labeled. Start with relay receipts only if you want to avoid this. |
| D3 | Which HCS events ship first? | Relay usage receipts. They have no other record. |
| D4 | Amend the `AGENTS.md` key rule to cover an operator's own key on their own relay? | Yes, with wording that still forbids the project hosting a key for others. |

## Order of work

1. Decide D1 to D4 and mark this ADR Decided.
2. Credit contract and `price` record on Sepolia, with tests.
3. Relay credit enforcement behind a flag, with a test that forwarding stops at zero credit and peers still connect directly.
4. UI top-up and balance.
5. HCS relay receipts, then access decisions if D2 allows.

Each step is tested before the next starts. This ADR describes none of it as working.

## Consequences

- Third parties can run a relay and earn from it, which removes the single subsidised relay as the only option.
- Members need MockUSDC and a deposit transaction before using a paid relay. A free relay still works with none.
- The relay becomes a component that reads chain state, so it needs RPC access and a cache like the node's registry cache.
- Audit records exist only for events a poster chooses to post. They are not a complete log.
