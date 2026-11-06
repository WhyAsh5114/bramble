import { describe, expect, it } from 'vitest'
import { parseMessage, verifyOpening } from '../src/commitment.ts'
import { MirrorTopicReader, hashscanMessageUrl, hashscanTopicUrl, verifyEntries } from '../src/mirror.ts'
import { AUDIT_SECRET_NAMES, missingAuditSecrets } from '../src/secrets.ts'
import { HcsAuditTrail, TOPIC_MEMO, createAuditTopic } from '../src/trail.ts'
import type { AuditEvent } from '../src/types.ts'
import { FakeTopicClient, fakeFetch } from './fakes.ts'

const grant: AuditEvent = {
  kind: 'grant',
  subject: 'agent-1.acme.eth',
  serviceDigest: `0x${'ab'.repeat(32)}`,
  expiresAt: '1893456000',
  txRef: `0x${'cd'.repeat(32)}`,
}
const revoke: AuditEvent = {
  kind: 'revoke',
  subject: 'agent-1.acme.eth',
  serviceDigest: `0x${'ab'.repeat(32)}`,
  txRef: `0x${'ef'.repeat(32)}`,
}
const payment: AuditEvent = {
  kind: 'payment',
  payer: '0.0.1001',
  payee: '0.0.1002',
  asset: '0.0.429274',
  amountAtomic: '1000',
  txRef: '0.0.1001@1700000000.1',
}

describe('createAuditTopic', () => {
  it('creates a topic with the audit memo and returns its ID', async () => {
    const client = new FakeTopicClient()
    expect(await createAuditTopic(client)).toBe('0.0.9000')
    expect(client.topics).toEqual([{ topicId: '0.0.9000', memo: TOPIC_MEMO }])
  })

  it('rejects a malformed topic ID from the client', async () => {
    const client = {
      createTopic: async () => ({ topicId: 'nope' }),
      submitMessage: async () => ({ transactionId: '', sequenceNumber: '' }),
    }
    await expect(createAuditTopic(client)).rejects.toThrow(/malformed/)
  })
})

describe('HcsAuditTrail', () => {
  it('posts one commitment per grant, revoke and payment, and returns the openings', async () => {
    const client = new FakeTopicClient()
    const trail = new HcsAuditTrail({ client, topicId: '0.0.9000' })
    const records = [await trail.record(grant), await trail.record(revoke), await trail.record(payment)]

    expect(client.submitted).toHaveLength(3)
    expect(records.map((r) => r.receipt.sequenceNumber)).toEqual(['1', '2', '3'])
    records.forEach((record, i) => {
      const posted = parseMessage(client.submitted[i].message)
      expect(posted).toEqual(record.message)
      expect(verifyOpening(posted!, record.opening)).toBe(true)
      expect(record.receipt.topicId).toBe('0.0.9000')
    })
    expect(records.map((r) => r.message.kind)).toEqual(['grant', 'revoke', 'payment'])
  })

  it('never posts plaintext, even from events that carry service and account names', async () => {
    const client = new FakeTopicClient()
    const trail = new HcsAuditTrail({ client, topicId: '0.0.9000' })
    for (const event of [grant, revoke, payment]) await trail.record(event)
    for (const { message } of client.submitted) {
      const text = new TextDecoder().decode(message)
      for (const secret of ['agent-1', 'acme', '0xabab', '0.0.1001', '0.0.429274', '1893456000', '1000']) {
        expect(text).not.toContain(secret)
      }
    }
  })

  it('posts different bytes for the same event twice', async () => {
    const client = new FakeTopicClient()
    const trail = new HcsAuditTrail({ client, topicId: '0.0.9000' })
    await trail.record(revoke)
    await trail.record(revoke)
    expect(Buffer.from(client.submitted[0].message).equals(Buffer.from(client.submitted[1].message))).toBe(false)
  })

  it('uses the injected randomness for the salt', async () => {
    const client = new FakeTopicClient()
    const trail = new HcsAuditTrail({ client, topicId: '0.0.9000', randomBytes: () => new Uint8Array(32).fill(1) })
    expect((await trail.record(grant)).opening.salt).toBe('01'.repeat(32))
  })

  it('propagates a failed submit and posts nothing', async () => {
    const client = new FakeTopicClient()
    client.failNextSubmit = true
    const trail = new HcsAuditTrail({ client, topicId: '0.0.9000' })
    await expect(trail.record(grant)).rejects.toThrow(/simulated/)
    expect(client.submitted).toHaveLength(0)
  })

  it('rejects an invalid event before posting', async () => {
    const client = new FakeTopicClient()
    const trail = new HcsAuditTrail({ client, topicId: '0.0.9000' })
    await expect(trail.record({ ...grant, subject: '' } as AuditEvent)).rejects.toThrow(/subject/)
    expect(client.submitted).toHaveLength(0)
  })

  it('rejects a malformed topic ID', () => {
    expect(() => new HcsAuditTrail({ client: new FakeTopicClient(), topicId: 'abc' })).toThrow(/topicId/)
  })
})

describe('MirrorTopicReader and verifyEntries', () => {
  async function postedTopic() {
    const client = new FakeTopicClient()
    const trail = new HcsAuditTrail({ client, topicId: '0.0.9000' })
    const records = [await trail.record(grant), await trail.record(revoke), await trail.record(payment)]
    return { client, records }
  }

  it('reads entries in order and verifies them against the openings', async () => {
    const { client, records } = await postedTopic()
    const fetch = fakeFetch(client.mirrorBody('0.0.9000'))
    const reader = new MirrorTopicReader({ mirrorBaseUrl: 'https://mirror.test/', fetch })
    const entries = await reader.listEntries('0.0.9000', { afterSequence: 0, limit: 10 })

    const url = new URL(fetch.urls[0])
    expect(url.pathname).toBe('/api/v1/topics/0.0.9000/messages')
    expect(url.searchParams.get('order')).toBe('asc')
    expect(url.searchParams.get('limit')).toBe('10')
    expect(url.searchParams.has('sequencenumber')).toBe(false)

    const verified = verifyEntries(
      entries,
      records.map((r) => r.opening)
    )
    expect(verified.map((e) => e.status)).toEqual(['verified', 'verified', 'verified'])
    expect(verified.map((e) => e.message?.kind)).toEqual(['grant', 'revoke', 'payment'])
    expect(verified[0].hashscanUrl).toBe(hashscanMessageUrl('1700000000.000000001'))
    expect(verified[0].opening).toEqual(records[0].opening)
  })

  it('sends a sequencenumber filter only for a positive cursor and rejects invalid cursors', async () => {
    const client = new FakeTopicClient()
    const fetch = fakeFetch(client.mirrorBody('0.0.9000'))
    const reader = new MirrorTopicReader({ mirrorBaseUrl: 'https://mirror.test', fetch })
    await reader.listEntries('0.0.9000', { afterSequence: 1 })
    expect(new URL(fetch.urls[0]).searchParams.get('sequencenumber')).toBe('gt:1')
    for (const afterSequence of [-1, 1.5, Number.NaN]) {
      await expect(reader.listEntries('0.0.9000', { afterSequence })).rejects.toThrow('afterSequence')
    }
    await expect(reader.listEntries('0.0.9000', { limit: 0 })).rejects.toThrow('limit')
  })

  it('marks an entry unopened when no opening matches, and malformed when it is not an audit message', async () => {
    const { client, records } = await postedTopic()
    const body = client.mirrorBody('0.0.9000')
    body.messages.push({
      consensus_timestamp: '1700000000.000000009',
      message: Buffer.from('spam').toString('base64'),
      sequence_number: 4,
      payer_account_id: '0.0.5',
    })
    const reader = new MirrorTopicReader({ mirrorBaseUrl: 'https://mirror.test', fetch: fakeFetch(body) })
    const verified = verifyEntries(await reader.listEntries('0.0.9000'), [records[0].opening])
    expect(verified.map((e) => e.status)).toEqual(['verified', 'unopened', 'unopened', 'malformed'])
    expect(verified[3].payerAccountId).toBe('0.0.5')
  })

  it('does not verify an entry against a forged opening', async () => {
    const { client, records } = await postedTopic()
    const forged = { ...records[0].opening, event: { ...grant, subject: 'agent-2.acme.eth' } as AuditEvent }
    const reader = new MirrorTopicReader({
      mirrorBaseUrl: 'https://mirror.test',
      fetch: fakeFetch(client.mirrorBody('0.0.9000')),
    })
    const [first] = verifyEntries(await reader.listEntries('0.0.9000'), [forged])
    expect(first.status).toBe('unopened')
  })

  it('throws on a mirror node error and tolerates an empty topic', async () => {
    const failing = new MirrorTopicReader({ mirrorBaseUrl: 'https://mirror.test', fetch: fakeFetch({}, 503) })
    await expect(failing.listEntries('0.0.9000')).rejects.toThrow(/503/)
    const empty = new MirrorTopicReader({ mirrorBaseUrl: 'https://mirror.test', fetch: fakeFetch({}) })
    expect(await empty.listEntries('0.0.9000')).toEqual([])
  })

  it('builds HashScan links', () => {
    expect(hashscanTopicUrl('0.0.9000')).toBe('https://hashscan.io/testnet/topic/0.0.9000')
  })
})

describe('missingAuditSecrets', () => {
  it('names the secrets that are absent and never echoes values', () => {
    expect(missingAuditSecrets({})).toEqual([...AUDIT_SECRET_NAMES])
    expect(missingAuditSecrets({ HEDERA_CLIENT_ACCOUNT_ID: '0.0.1' })).toEqual(['HEDERA_CLIENT_PRIVATE_KEY'])
    expect(missingAuditSecrets({ HEDERA_CLIENT_ACCOUNT_ID: '0.0.1', HEDERA_CLIENT_PRIVATE_KEY: 'k' })).toEqual([])
  })
})
