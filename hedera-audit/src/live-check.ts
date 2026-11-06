// Live Hedera testnet run: creates a topic, posts one commitment per event kind, reads the topic
// back from the mirror node and checks that each opening verifies. Needs the payer secrets.
import { createHieroTopicClient } from './hiero.ts'
import { MirrorTopicReader, hashscanTopicUrl, verifyEntries } from './mirror.ts'
import { missingAuditSecrets } from './secrets.ts'
import { HcsAuditTrail, createAuditTopic } from './trail.ts'
import type { AuditEvent } from './types.ts'

const MIRROR_TESTNET = 'https://testnet.mirrornode.hedera.com'
const MIRROR_WAIT_MS = 8_000

const missing = missingAuditSecrets(process.env)
if (missing.length > 0) {
  console.error(`BLOCKED: missing secret(s): ${missing.join(', ')}. Live HCS run needs human verification.`)
  process.exit(2)
}

const client = createHieroTopicClient({
  accountId: process.env.HEDERA_CLIENT_ACCOUNT_ID!,
  privateKey: process.env.HEDERA_CLIENT_PRIVATE_KEY!,
  network: 'testnet',
})
const topicId = await createAuditTopic(client)
console.log(`topic ${topicId} ${hashscanTopicUrl(topicId)}`)

const trail = new HcsAuditTrail({ client, topicId })
const events: AuditEvent[] = [
  {
    kind: 'grant',
    subject: 'agent-1.example.eth',
    serviceDigest: `0x${'ab'.repeat(32)}`,
    expiresAt: '1893456000',
    txRef: `0x${'01'.repeat(32)}`,
  },
  {
    kind: 'revoke',
    subject: 'agent-1.example.eth',
    serviceDigest: `0x${'ab'.repeat(32)}`,
    txRef: `0x${'02'.repeat(32)}`,
  },
  {
    kind: 'payment',
    payer: '0.0.1001',
    payee: '0.0.1002',
    asset: '0.0.429274',
    amountAtomic: '1000',
    txRef: '0.0.1001@1.1',
  },
]
const records = []
for (const event of events) records.push(await trail.record(event))
console.log(`posted ${records.length} messages, last sequence ${records.at(-1)!.receipt.sequenceNumber}`)

await new Promise((resolve) => setTimeout(resolve, MIRROR_WAIT_MS))
const entries = await new MirrorTopicReader({ mirrorBaseUrl: MIRROR_TESTNET }).listEntries(topicId)
const verified = verifyEntries(
  entries,
  records.map((r) => r.opening)
)
for (const e of verified) console.log(`#${e.sequenceNumber} ${e.message?.kind ?? '?'} ${e.status} ${e.hashscanUrl}`)
if (verified.length !== events.length || verified.some((e) => e.status !== 'verified')) {
  console.error('FAILED: not every posted entry verified')
  process.exit(1)
}
console.log('OK')
