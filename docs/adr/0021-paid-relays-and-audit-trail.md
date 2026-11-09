# ADR 0021 — Paid relays over x402 and an HCS audit trail on Hedera, both opt-in

**Kind:** Explanation. **Status:** Proposed, 10 Nov 2026. The maintainer decided that Hedera returns as the payment rail and the audit chain, that both features are opt-in, and that the node may hold its own Hedera key. Paid relays, the node's opt-in key and the HCS receipt log are built; gateway access decisions on HCS are not. Amends [ADR 0019](0019-user-driven-tailnets.md), which retired x402 metering and the HCS audit trail from the main tree. Evidence labels: **Verified** (ran it), **Read from source** (read the code or docs, did not run it) and **Open** (undecided or untested).

## Context

The Mumbai build won the Hedera track with x402 payments on Hedera testnet, settled through the Blocky402 facilitator. The relay was the x402-gated service and the agent node paid for relay bandwidth. The track also rewarded metering by use, a findable service directory and HCS (Hedera Consensus Service) audit trails (`docs/archive/hackathon/02_TRACK_FIT.md`, **Read from source**).

ADR 0019 removed that work because it depended on a hosted approval service and an operator who paid for everything. Two needs remain:

- **Relay cost.** The project relay stops forwarding when its byte budget runs out (`relay/relaysrv/budget.go`, **Read from source**). A third party can run a relay and list it, but has no way to be paid.
- **Audit.** Grants and revokes are public ENS records. Relay usage and gateway access decisions have no shared record. A gateway's allow and deny decisions live only in that node's local activity log.

The archived designs are in the `archive/hackathon` tag: [ADR 0007](0007-rendezvous-relay-metering.md) and [ADR 0008](0008-data-relay-metering.md) for x402 payments, and [ADR 0014](0014-hcs-audit-trail.md) for HCS. ADR 0008 records real Hedera testnet settlements across two relay processes on two machines, confirmed on the mirror node (**Read from source**).

## Decision

1. **Restore the x402 flow, not a custom one.** The relay serves paid routes that answer `402 Payment Required`. The node pays with the x402 `exact` scheme on `hedera:testnet`, and the Blocky402 facilitator verifies the payment, settles it and pays the network fee. This is the flow the track rewarded.
2. **Both features are opt-in.** A node, tailnet or relay that does not opt in needs no Hedera account. The free relay path works with none.
3. **Payment and audit never gate admission.** Peers still enforce access from ENS state. A failed payment stops one paid relay serving that payer. A failed audit post changes nothing (ADR 0014, decision 5).
4. **Hedera testnet only.** `AGENTS.md` forbids mainnet writes and real money. Mainnet needs a new decision.
5. **The node may hold its own Hedera key.** It is a separate key from the tailnet wallet and cannot write tailnet records. `AGENTS.md` now allows it, and allows a Hedera testnet key for live runs.
6. **Port x402 to Go.** The product is one Go binary. The archived client and relay sidecar were TypeScript, and no Go x402 implementation was found on the Hedera pages checked. The Go port covers the 402 server on the relay and the signing client on the node. **Open:** whether a Go implementation exists elsewhere.

## The payment flow

### What the archive did

**Read from source:** the node's sidecar called the relay's sidecar. For a rendezvous token it paid `POST /rendezvous-token`. For a data-relay session it paid `POST /data-relay-session?bytes=N`, priced by the byte count. The relay sidecar minted a token or session and handed it to the Go relay over loopback. The client used `@x402/fetch` with an `ExactHederaScheme` signer over an ECDSA key, with a per-payment spend cap.

### What changes

1. The Go relay serves the paid routes itself. There is no TypeScript sidecar and no loopback handoff.
2. The node's Go client answers a 402 by building a partially signed Hedera `TransferTransaction` and retrying with it. The facilitator is the fee payer, so the node needs the token but no HBAR for fees.
3. The relay asks the facilitator to verify and settle, then mints the token or session as before.
4. The relay prices each request. A data-relay session costs by the byte count in the request, as in ADR 0008.
5. A relay advertises its price and pay-to account. A node picks among relays by price and latency, as in the archived selection. **Open:** where the price and account live in the relay record, and the exact ordering against the free relay.

### Transport

The relay serves the paid route over HTTP on its existing TLS port. It looks at the first bytes of each connection: a `POST ` or `GET ` request line goes to the x402 routes, and anything else is the rendezvous protocol. No new port opens, so the security group, `compose.yml` and `ufw` stay as they are. The node reaches the route with an HTTP client that checks the relay's TLS key against its published pin, so a payment goes only to a relay that was listed. The route is `POST /x402/session?bytes=N`, answered with `402`, then a single-use token that the node presents in the rendezvous `session` message.

### Built and tested on 10 Nov 2026 (offline)

- **Relay (Go), Verified:** `relay/relaysrv/x402.go`. It builds its own requirements and sends only those to the facilitator, rejects a client's changed `accepted` copy, settles before it mints, and issues a token that works once and expires after 10 minutes. Paid mode is off by default and the free path is unchanged. The tests use a fake facilitator.
- **Node (Go), Verified:** `brambled/hederapay`. The transaction layout follows the reference client (`@x402/hedera` 2.25.0) and was checked against the spec's verification rules: the transaction ID belongs to the fee payer, net token transfers are zero, `payTo` is credited exactly `amount`, the fee payer is never debited, and one signature (the payer's) is present. A spend cap and a testnet-only rule are enforced.
- **Whole flow, Verified:** the real relay, the real client and the pin-checked HTTP client, with a facilitator stand-in that applies the spec's rules and refuses a repeated transaction ID. A payment signed by the wrong key buys nothing.
- **Live payment, Verified 10 Nov 2026 (HBAR):** the real relay, the real client and the live Blocky402 testnet facilitator. A 1,000 tinybar payment settled, the node received a token and opened a session, and the public mirror node shows the payer at -1000 and the pay-to account at +1000 with the facilitator paying the fee. The tests are `brambled/hederapay/live_test.go` (build tag `live`). Two bugs surfaced only against the live facilitator and are fixed: its `/supported` reply holds arrays in `extra`, and it answers `transaction_could_not_be_decoded` to a transaction frozen for all seven testnet nodes. A payment is now frozen for three nodes, as the reference client does.
- **Not verified:** a payment in USDC. The live run used HBAR because the faucet's USDC had not reached the test accounts. The token variant differs only in `AddTokenTransfer` against `AddHbarTransfer`, and the offline layout tests cover it, but the live facilitator has not seen it.

### Verified on 10 Nov 2026

The Blocky402 testnet facilitator at `api.testnet.blocky402.com` answers `/supported` with the `exact` scheme on `hedera:testnet`, x402 version 2, fee payer `0.0.7162784`, and two executor accounts.

### Not yet read

The wire-level details come from the x402 Hedera scheme spec, which has not been read yet: header names, the `PaymentRequirements` and `PaymentPayload` fields, the facilitator `/verify` and `/settle` bodies, the transfer shape and replay protection. The Hedera docs pages describe the shape only. They show a base64 partially signed transaction in `payload.transaction` and a fixed price per resource. The archived TypeScript used the `PAYMENT-REQUIRED` and `PAYMENT-RESPONSE` headers, while one Hedera page names `X-Payment`. Step 2 of the order of work resolves this before any code is written.

### Hedera Go SDK, verified 10 Nov 2026

`github.com/hiero-ledger/hiero-sdk-go/v2` v2.85.1 builds with `CGO_ENABLED=0` for linux/arm64, linux/amd64 and darwin/arm64. A probe program generated a key, built a fungible `TransferTransaction` with `AddTokenTransfer`, set a transaction memo and built a `TopicMessageSubmitTransaction`. The probe did not contact a network or test partial signing. A probe binary with the SDK is 25 to 27 MB uncompressed, so it will enlarge the release binary. **Open:** partial signing with a different fee payer, the real size increase in `bramble`, and the SDK's planned removal of deprecated APIs. The SDK has no general mirror REST client, so lookups use plain HTTPS.

## The node's Hedera key

- The key is ECDSA secp256k1, as in the archived client. The node generates or imports it on opt-in and stores it in the state directory with mode `0600`. The UI shows the account ID and balance, never the key.
- The user funds the account. A node that does not opt in holds no Hedera key.
- A spend cap limits each payment, as `HEDERA_MAX_PAYMENT_ATOMIC` did in the archive. **Open:** a daily cap.
- The key pays only the relay's price. It holds no tailnet role.

### Accounts that already exist

Two Hedera testnet accounts from the Mumbai runs still exist (**Verified**, read from the public mirror node on 10 Nov 2026). Both are ECDSA secp256k1 accounts and are associated with USDC.

| Account | Role in the runs | HBAR | USDC (token `0.0.429274`, smallest units) |
|---|---|---|---|
| `0.0.10433667` | payer | 19.36 | 20,410,000 |
| `0.0.10433704` | pay-to | 9.38 | 19,590,000 |

Their private keys are not in this repository or in the `.env` file. The maintainer supplies a key for live runs.

## HCS audit trail

### What to record

Grants and revokes are already public in ENS records, so this ADR does not post them. Two kinds of event have no shared record today:

1. **Relay usage receipts.** A paid relay posts a commitment for each settled payment: payer, bytes and price. The payment is on Hedera, so the receipt sits next to it.
2. **Gateway access decisions.** A tailnet that opts in posts each allow and deny for a service.

The record format stays as merged for ADR 0014: `{"v":1,"kind":…,"c":…}`, where `c` is an HMAC-SHA256 commitment keyed by a fresh per-entry salt. Never post a plaintext service name, host or port. The holder of the openings reveals one to a verifier. **Read from source:** `hedera-audit/` and `docs/16_HCS_AUDIT_RECORD_FORMAT.md` in the archive tag.

### Who posts

- **A relay operator** who opts in posts receipts to a topic they own, with their own Hedera key.
- **A tailnet** that opts in creates a topic and records its ID in a tailnet record. Each gateway posts to it with its node's Hedera key. **Open:** the tailnet record name, and who may read the openings. The old design held them in the approval service, which no longer exists.

A failed post never blocks traffic or changes an access decision.

### What a mirror match proves

Carried over from ADR 0014. A match shows the mirror node returned bytes whose commitment the opening reproduces. It does not prove the event happened, and it does not prove the log is complete, because the poster can omit events.

## Fee comparison

Figures from public sources on 10 Nov 2026. Ethereum figures come from gas trackers, which disagree with each other and move with gas and ETH price. Treat them as estimates. None is a measurement made in this repo.

| Operation | Hedera | Ethereum mainnet |
|---|---|---|
| Token transfer (USDC) | $0.001 (`TokenTransfer`) | about $0.01 to $0.06 (an ERC-20 transfer uses about 65,000 gas; trackers show 0.13 to 0.35 gwei) |
| Log one message | $0.0008 (`ConsensusSubmitMessage`, no custom fees) | A contract event costs gas like any call. No tracker figure was checked. |
| One-time setup | $0.05 to associate an account with a token (`TokenAssociate`) | None for ERC-20 |
| Testnet | Identical fee schedule, paid in free test HBAR | Free test ETH on Sepolia |

What this shows:

- Hedera is about 10 to 60 times cheaper per token transfer, and its fee is fixed in USD. Ethereum's fee depends on gas, which has been low in late 2026 but can spike.
- Both are cheap in absolute terms. Hedera's stronger advantages are the predictable USD fee, a logging price that suits many small messages, and the facilitator paying the fee for the payer.
- The payer and the relay operator each pay $0.05 once to associate with USDC. Circle's testnet faucet supplies Hedera testnet USDC. Faucet limits conflict between sources and are **Open**.
- A layer 2 network would probably be cheaper than Ethereum mainnet. It was not compared here.
- On testnets both chains are free, so the comparison matters only if payments move to mainnet.

Sources: [Hedera fees](https://docs.hedera.com/networks/fees), [the January 2026 HCS price update](https://hedera.com/blog/price-update-to-consensussubmitmessage-in-consensus-service-january-2026/), [a gas tracker snapshot of 9 Nov 2026](https://chaingate.dev/gas-tracker/ethereum), [a second tracker, 4 Nov 2026](https://chainscorelabs.com/fees/ethereum), [Circle's USDC on Hedera page](https://www.circle.com/multi-chain-usdc/hedera).

## Alternatives considered

| Option | Why not |
|---|---|
| Direct token transfer verified through a mirror node | An earlier draft of this ADR proposed it. It is not the flow the track rewarded, and it makes the relay verify settlement itself. |
| Prepaid credit in an Ethereum contract | Needs a contract and Ethereum gas for every deposit, and drops Hedera from the payment path. |
| Keep the TypeScript sidecars | Breaks the one-binary rule and adds a second runtime to every node. |
| Payment channels | Correct, but far more work than this relay needs. |

## Decisions

| ID | Question | Status |
|---|---|---|
| D1 | Payment rail | **Decided:** Hedera testnet, x402 `exact` through Blocky402, opt-in. |
| D2 | May an opt-in node hold a Hedera key? | **Decided:** yes, a separate key that cannot touch tailnet records. |
| D3 | Amend the `AGENTS.md` key rules | **Decided:** done in the PR that adds this ADR. |
| D4 | Mainnet or real money for Hedera payments | **Open:** testnet only for now. |

## Order of work

1. Settle D3 and mark this ADR Decided. D3 is in this PR.
2. Read the x402 Hedera scheme spec. Spike a Go client against the testnet facilitator, using the funded account the maintainer supplies. This resolves the Open items under "Not yet read" and the partial-signing question.
3. Measure the release-binary size with the Hiero Go SDK added.
4. Relay: the 402 routes and facilitator calls in Go, priced per byte, behind a flag. Test that forwarding stops without payment and that peers still connect directly.
5. Node and UI: opt-in switch, key handling, funding view and automatic payment.
6. HCS: relay receipts, then gateway access decisions.

Each step is tested before the next starts. This ADR describes none of it as working.

## Consequences

- Third parties can run a relay and be paid, so the single subsidised relay is no longer the only option.
- An opting-in node holds a Hedera key and needs a funded Hedera account. A node that does not opt in is unchanged.
- The relay depends on the Blocky402 facilitator. If it goes away, the relay needs another facilitator or its own.
- Audit records exist only for events a poster chooses to post. They are not a complete log.


## Implementation status, 10 Nov 2026

- **Node:** `-hedera-account` and `-hedera-key` turn payment on (`brambled/pay.go`). A free relay always wins; a relay that charges is used only when no free one can serve, and its status reply (not the directory's price text) gives the price. The node trims a session to fit `-pay-max-session` and `-pay-max-day` (defaults 0.5 and 2 USDC), refuses any asset but testnet USDC and any network but `hedera:testnet`, and keeps spent totals and recent payments in memory only. **Verified** offline against real paid relays with a stub facilitator (`brambled/pay_test.go`).
- **Daemon and UI:** Settings, "Paying relays" makes the device's own ECDSA key (`hedera.json`, mode 0600) when the user opts in. Funding the printed 0x address creates the account (auto-creation); the daemon finds the account ID and balances through the public mirror node and restarts the node with payment on. The Relays page shows each relay's price and receipt topic. **Verified** in a real browser, light and dark; the live funded path is **Open**.
- **HCS receipts (relay):** with a paid relay, `RELAY_HEDERA_ACCOUNT`, `RELAY_HEDERA_KEY` and optionally `RELAY_HCS_TOPIC`, each sold session writes a receipt (`tx`, `bytes`, `amount`, `asset`, `network`, `at`) to a topic only the relay can write to. Receipts hold no payer or peer, and a slow Hedera never blocks a session. **Verified live** on testnet: topic created, receipt read back from the mirror node.
- **Size:** linking the SDK takes the `bramble` binary from 11.3 MB to 27.9 MB (stripped, `CGO_ENABLED=0`, darwin/arm64). **Verified** by building both.
- **Directory price:** a relay may publish `price` as `0` or `paid`. It is advisory; a node that does not pay skips any relay whose status shows a price.
- **Not done:** gateway access decisions on HCS (needs a topic record in the tailnet and HBAR on each node); a USDC payment on the live facilitator; mainnet.
