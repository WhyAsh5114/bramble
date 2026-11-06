# @bramble/hedera-audit

Posts a salted hash commitment to a Hedera Consensus Service (HCS) topic for each grant, revoke and payment. Never plaintext. Record format: `docs/16_HCS_AUDIT_RECORD_FORMAT.md`. Design options: `docs/adr/0011-w10-hedera-service-options.md`.

## How to record an event

```ts
import { HcsAuditTrail, createAuditTopic } from '@bramble/hedera-audit'
import { createHieroTopicClient } from '@bramble/hedera-audit/hiero'

const client = createHieroTopicClient({ accountId, privateKey, network: 'testnet' })
const topicId = await createAuditTopic(client) // once; store the ID in config
const trail = new HcsAuditTrail({ client, topicId })

const { opening, receipt } = await trail.record({
  kind: 'revoke',
  subject: 'agent-1.acme.eth',
  serviceDigest: '0x…',
  txRef: '0x…',
})
// Keep `opening`. Show it to a verifier. Never post it.
```

The caller owns `accountId` and `privateKey`. The module reads no environment variable except in `src/live-check.ts`.

## How to verify entries

```ts
import { MirrorTopicReader, verifyEntries } from '@bramble/hedera-audit'

const reader = new MirrorTopicReader({ mirrorBaseUrl })
const entries = await reader.listEntries(topicId)
const rows = verifyEntries(entries, openings) // status: verified | unopened | malformed
```

## How to test

Tests inject a fake `TopicClient` and a fake `fetch`. No network or secret is needed.

```sh
pnpm --filter @bramble/hedera-audit test
pnpm --filter @bramble/hedera-audit typecheck
```

## How to run against Hedera testnet

Needs `HEDERA_CLIENT_ACCOUNT_ID` and `HEDERA_CLIENT_PRIVATE_KEY` in the environment. Provided by a human (checkpoint H3).

```sh
pnpm --filter @bramble/hedera-audit live-check
```

## Layout

| Path                | Purpose                                                                      |
| ------------------- | ---------------------------------------------------------------------------- |
| `src/types.ts`      | Event, opening, message and `TopicClient` types                              |
| `src/commitment.ts` | Encoding, salted commitment, message parse, opening verification             |
| `src/trail.ts`      | `HcsAuditTrail` and `createAuditTopic`, built on an injected `TopicClient`   |
| `src/mirror.ts`     | Mirror node reader, HashScan links, `verifyEntries`                          |
| `src/hiero.ts`      | Real `TopicClient` on `@hiero-ledger/sdk`, testnet and previewnet only       |
| `src/secrets.ts`    | Names of the secrets the live run needs                                      |
| `src/live-check.ts` | Live testnet run; exits 2 and names the missing secrets when they are absent |
