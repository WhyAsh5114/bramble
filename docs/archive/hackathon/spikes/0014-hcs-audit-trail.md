# HCS audit trail spike evidence

**Kind:** Reference. ETH-66 (workstream W10, demo beat 3). Recorded 6 November 2026 on Linux x64, Node 24.15.0 and Bun 1.3.14. Design: [ADR 0014](../adr/0014-hcs-audit-trail.md), draft; human approval pending. The spike never connects to Hedera testnet, creates a topic, signs or submits a transaction.

| Finding                                                                                                                                                                                              | Label                                                                                                                                                                                                              |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A merged-format message (`buildAudit` and `encodeMessage`; 94 bytes for a `revoke`) fits a frozen `TopicMessageSubmitTransaction`, round-trips through transaction bytes, parses with `parseMessage` and verifies with `verifyOpening` | **Verified:** `hedera-audit/test/sdk-freeze.test.ts` (Node, part of `pnpm test`) and the same function under Bun. SDK is the workspace's `@hiero-ledger/sdk` 2.85.0                                                  |
| The frozen transaction matches what `createHieroTopicClient` builds for `submitMessage`, apart from the loopback client, node and transaction ID the check fixes, plus `setMaxChunks(1)` and a fee cap | **Read from source:** `hedera-audit/src/hiero.ts`                                                                                                                                                                  |
| Mirror listing, cursor handling, base64 decoding and entry verification                                                                                                                              | **Verified:** existing unit tests in `hedera-audit/test/trail.test.ts` against an injected `fetch`. A reviewer also ran the list route on public testnet (`docs/16`). Not rerun here                               |
| Submission is `execute(client)` then `getReceipt(client)`; the receipt exposes the topic sequence number                                                                                             | **Read from source:** `hedera-audit/src/hiero.ts` and the [Hedera submit-message documentation](https://docs.hedera.com/native/consensus/submit-message). Not executed                                              |
| HashScan topic page is `/testnet/topic/{topicId}`                                                                                                                                                    | **Read from source:** `hashscanTopicUrl` in `hedera-audit/src/mirror.ts`                                                                                                                                           |
| HashScan message page: the merged reader builds `/testnet/transaction/{consensusTimestamp}`; the explorer source at commit `92a95cc85bf90dfbfaf9456aa2e9ede3ff7415ed` also routes `/testnet/transaction/{consensusTimestamp}/message` | **Read from source** for both; the second was recorded by the original ETH-66 spike and not re-read here. Which link shows the message better is **Open** until a browser check of a real entry                    |
| Live submission, receipt status, topic permissions, mirror ingestion delay, explorer rendering, payment settlement hook                                                                              | **Open:** needs H3 (payer, topic and signing setup)                                                                                                                                                                |

## Limits

- The check freezes and serializes only. It does not prove the network accepts the message, charges the expected fee or orders it.
- It asserts that the topic ID, the message bytes, the parsed kind and the opening match. It has no negative cases; the merged unit tests cover malformed messages and wrong openings.
- It does not exercise `createHieroTopicClient`, which needs a real key and client.

## Reproduce

From the repository root, after `pnpm install --frozen-lockfile`:

```sh
pnpm --filter @bramble/hedera-audit test
cd hedera-audit && bun -e "import('./test/freeze-round-trip.ts').then(m => console.log(m.freezeRoundTrip()))"
```

Both exit 0. The Bun command prints `{ messageBytes: 94 }`.
