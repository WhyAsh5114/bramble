# ADR 0011 — W10: which new Hedera service to build

**Status:** Draft, 6 Nov 2026. **Human approver:** not yet named. Decision belongs to a human (checkpoint H1 in `docs/15_MUMBAI_HANDOFF.md`).

## Context

The Hedera continuity prize judges only new work. Bramble already settles x402 relay payments on Hedera through Blocky402. Showing those payments in the console is polish and does not count. The handoff (section 5.2) lists three options and says a human asks the Hedera judge which one counts. As of this draft, the judge has not answered. The handoff's default is option A, and says to use it if there is no answer by T-28.

Only option A is implemented. This ADR records the three options so a human can confirm or change the choice.

Evidence labels: Verified (ran it), Read from source (code or docs, not run), Open (not tested).

## Options

### A. HCS audit trail (implemented)

Each grant, revoke and payment posts a salted hash commitment to a Hedera Consensus Service topic. This PR ships the standalone module, a mirror node reader and a live check. Planned, not built: the console reads the topic through the reader and verifies entries against openings that the approval service keeps.

- **Demo moment:** beat 3 shows the topic entry with a HashScan link.
- **Privacy:** the topic shows event kind, counts, timing and the payer and topic identities, so activity can be correlated. Encoded fields (subjects, services, amounts) are hidden only while each salt stays secret (`docs/16_HCS_AUDIT_RECORD_FORMAT.md`). Answer "Isn't a public Hedera log a privacy leak?" with "Only salted hash commitments are posted, and the kind and timing are public."
- **Invariants:** no topology on-chain (commitments only), no local-state writes (the module lives outside `sidecar/src`), no enforcement role (peers do not read the topic).
- **Cost and risk:** one HCS transaction per event, testnet only. Needs the payer secrets for a live run. Planned: the approval service must keep openings for verification; if it holds them only in memory, a restart loses them.
- **Status:** module and unit tests with a fake client: Verified. Live testnet run: Open.
- **Weakness:** an audit log is not a service in the sense of a new capability. The judge may read it as a thin addition. The submit-key holder can omit events.

### B. Scheduled Transactions (not implemented)

Relay usage accumulates, and one Hedera scheduled transaction settles each period.

- **Demo moment:** beat 3 shows the scheduled settlement executing.
- **Fit:** closest to the existing payment flow, so it changes how Bramble pays and not only what it logs.
- **Cost and risk (Open):** x402 settles per request through Blocky402. Batching changes the payment protocol between the sidecar and the relay, touches `sidecar/src/payments/` and `relay-sidecar/`, and needs a trusted accounting of usage. It also weakens the per-request spend caps of ADR 0007 unless they are re-designed. Highest effort and highest risk to the working payment path.
- **Open:** whether a scheduled settlement executes inside the demo window, and whether the signer path works over Tor (handoff section 5.1).

### C. HCS-14 agent identity (not implemented)

The agent gets an on-chain identity on Hedera, linked to its ENS name.

- **Demo moment:** the request card shows the agent's Hedera identity.
- **Fit:** strong story for AI agents, and little code in the payment path.
- **Cost and risk (Open):** the exact HCS-14 record format and any registry or tooling are not checked. It adds a second identity next to ENS, which the project treats as the source of truth. The link between the two identities needs a trust rule that a human must define, and any public link between an ENS name and a Hedera account is a privacy cost.

## Comparison

| Criterion                      | A. Audit trail            | B. Scheduled settlement     | C. HCS-14 identity         |
| ------------------------------ | ------------------------- | --------------------------- | -------------------------- |
| Effort                         | Low                       | High                        | Medium (Open)              |
| Risk to working payment path   | None                      | High                        | None                       |
| Visible in the console         | Yes, HashScan link        | Yes, HashScan link          | Yes, identity on the card  |
| Privacy cost                   | Kind, timing, counts      | Aggregate amounts           | ENS-to-Hedera link         |
| Likely to count as "new" (Open)| Plausible                 | Strong                      | Plausible                  |

## Recommendation

Keep A as the default, as the handoff says. It is the lowest-risk option and it is built. Ask the Hedera judge (H1) before T-28. If the judge prefers B or C, the audit module still serves as the record of grants, revokes and payments, and the extra work moves to a new ticket.

## Decision needed from a human

1. Confirm A, or choose B or C.
2. Name the approver of this ADR, as the handoff requires.
3. Provide the Hedera testnet payer account (`HEDERA_CLIENT_ACCOUNT_ID`, `HEDERA_CLIENT_PRIVATE_KEY`) so the live run can be verified (checkpoint H3).
