import { createHmac } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import {
  buildAudit,
  commit,
  encodeEvent,
  encodeMessage,
  newSalt,
  parseMessage,
  verifyOpening,
} from '../src/commitment.ts'
import type { AuditEvent, GrantEvent, PaymentEvent, RevokeEvent } from '../src/types.ts'

const SALT = '11'.repeat(32)
const grant: GrantEvent = {
  kind: 'grant',
  subject: 'agent-1.acme.eth',
  serviceDigest: `0x${'ab'.repeat(32)}`,
  expiresAt: '1893456000',
  txRef: `0x${'cd'.repeat(32)}`,
}
const revoke: RevokeEvent = {
  kind: 'revoke',
  subject: 'agent-1.acme.eth',
  serviceDigest: grant.serviceDigest,
  txRef: `0x${'ef'.repeat(32)}`,
}
const payment: PaymentEvent = {
  kind: 'payment',
  payer: '0.0.1001',
  payee: '0.0.1002',
  asset: '0.0.429274',
  amountAtomic: '1000',
  txRef: '0.0.1001@1700000000.1',
}

describe('commit', () => {
  it('matches an independently computed HMAC over the documented encoding', () => {
    const u32 = (n: number) => Buffer.from([(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255])
    const lp = (s: string) => Buffer.concat([u32(Buffer.byteLength(s)), Buffer.from(s)])
    const expected = createHmac('sha256', Buffer.from(SALT, 'hex'))
      .update(
        Buffer.concat([
          lp('bramble-audit-v1'),
          lp('expiresAt'),
          lp(grant.expiresAt),
          lp('kind'),
          lp('grant'),
          lp('serviceDigest'),
          lp(grant.serviceDigest),
          lp('subject'),
          lp(grant.subject),
          lp('txRef'),
          lp(grant.txRef),
        ])
      )
      .digest('hex')
    expect(commit(grant, SALT)).toBe(expected)
  })

  it('is deterministic for the same event and salt', () => {
    expect(commit(grant, SALT)).toBe(commit({ ...grant }, SALT))
  })

  it('changes with the salt, so equal events are unlinkable', () => {
    expect(commit(grant, SALT)).not.toBe(commit(grant, '22'.repeat(32)))
  })

  it.each([grant, revoke, payment] as AuditEvent[])('changes when any field of a $kind event changes', (event) => {
    const base = commit(event, SALT)
    for (const name of Object.keys(event).filter((n) => n !== 'kind')) {
      const changed = { ...event, [name]: `${(event as unknown as Record<string, string>)[name]}x` } as AuditEvent
      expect(commit(changed, SALT)).not.toBe(base)
    }
  })

  it('cannot be confused by moving bytes between fields', () => {
    const a = { ...revoke, subject: 'ab', serviceDigest: 'c' }
    const b = { ...revoke, subject: 'a', serviceDigest: 'bc' }
    expect(encodeEvent(a).equals(encodeEvent(b))).toBe(false)
    expect(commit(a, SALT)).not.toBe(commit(b, SALT))
  })

  it('rejects malformed events and salts', () => {
    expect(() => commit({ ...grant, subject: '' }, SALT)).toThrow(/subject/)
    const missing: Partial<GrantEvent> = { ...grant }
    delete missing.txRef
    expect(() => commit(missing as AuditEvent, SALT)).toThrow(/txRef/)
    expect(() => commit({ ...grant, extra: 'x' } as unknown as AuditEvent, SALT)).toThrow(/unexpected field extra/)
    expect(() => commit({ kind: 'transfer' } as unknown as AuditEvent, SALT)).toThrow(/unknown audit kind/)
    expect(() => commit(grant, 'zz')).toThrow(/salt/)
  })
})

describe('newSalt', () => {
  it('draws 32 bytes from the injected source', () => {
    expect(newSalt(() => new Uint8Array(32).fill(7))).toBe('07'.repeat(32))
  })
  it('rejects a short source', () => {
    expect(() => newSalt(() => new Uint8Array(8))).toThrow(/32 bytes/)
  })
  it('uses distinct salts by default', () => {
    expect(newSalt()).not.toBe(newSalt())
  })
})

describe('message', () => {
  it('posts only version, kind and commitment, never plaintext', () => {
    for (const event of [grant, revoke, payment]) {
      const { message } = buildAudit(event)
      const text = new TextDecoder().decode(encodeMessage(message))
      expect(Object.keys(JSON.parse(text)).sort()).toEqual(['c', 'kind', 'v'])
      for (const value of Object.values(event)) {
        if (value !== event.kind) expect(text).not.toContain(value)
      }
      expect(text.length).toBeLessThan(100)
    }
  })

  it('round-trips through parseMessage', () => {
    const { message } = buildAudit(grant)
    expect(parseMessage(encodeMessage(message))).toEqual(message)
  })

  it.each([
    ['not json', 'nope'],
    ['an array', '[]'],
    ['wrong version', '{"v":2,"kind":"grant","c":"' + 'a'.repeat(64) + '"}'],
    ['unknown kind', '{"v":1,"kind":"transfer","c":"' + 'a'.repeat(64) + '"}'],
    ['short commitment', '{"v":1,"kind":"grant","c":"abc"}'],
    ['uppercase commitment', '{"v":1,"kind":"grant","c":"' + 'A'.repeat(64) + '"}'],
    ['extra field', '{"v":1,"kind":"grant","c":"' + 'a'.repeat(64) + '","subject":"x"}'],
  ])('rejects %s', (_name, text) => {
    expect(parseMessage(new TextEncoder().encode(text))).toBeNull()
  })

  it('rejects invalid UTF-8', () => {
    expect(parseMessage(new Uint8Array([0xff, 0xfe]))).toBeNull()
  })
})

describe('verifyOpening', () => {
  it('accepts the matching opening', () => {
    const { message, opening } = buildAudit(payment)
    expect(verifyOpening(message, opening)).toBe(true)
  })

  it('rejects a tampered field, a wrong salt, and a swapped kind', () => {
    const { message, opening } = buildAudit(grant)
    expect(verifyOpening(message, { ...opening, event: { ...grant, subject: 'agent-2.acme.eth' } })).toBe(false)
    expect(verifyOpening(message, { ...opening, salt: '33'.repeat(32) })).toBe(false)
    expect(verifyOpening({ ...message, kind: 'revoke' }, opening)).toBe(false)
    expect(verifyOpening(message, { ...opening, salt: 'bad' })).toBe(false)
  })

  it('rejects an opening for a different event with the same salt', () => {
    const { message } = buildAudit(grant, () => Buffer.from(SALT, 'hex'))
    expect(verifyOpening(message, { v: 1, event: revoke, salt: SALT })).toBe(false)
  })

  describe('event snapshot', () => {
    it('rejects fields that are inherited or not enumerable instead of omitting them from the commitment', () => {
      const inherited = Object.create({ ...grant }) as AuditEvent
      expect(() => commit(inherited, SALT)).toThrow()
      expect(() => buildAudit(inherited)).toThrow()

      const hidden = { ...grant } as Record<string, unknown>
      Object.defineProperty(hidden, 'txRef', { value: grant.txRef, enumerable: false })
      expect(() => commit(hidden as unknown as AuditEvent, SALT)).toThrow('txRef')

      const proto = Object.create({ kind: 'grant' }) as Record<string, unknown>
      Object.assign(proto, { subject: grant.subject, serviceDigest: grant.serviceDigest, expiresAt: '1', txRef: 'x' })
      expect(() => commit(proto as unknown as AuditEvent, SALT)).toThrow('unknown audit kind')
    })

    it('binds the kind into the commitment', () => {
      const asRevoke = { ...revoke, kind: 'grant' } as unknown as AuditEvent
      expect(() => commit(asRevoke, SALT)).toThrow('non-empty expiresAt')
      const fields = { subject: grant.subject, serviceDigest: grant.serviceDigest, txRef: grant.txRef }
      expect(commit({ kind: 'revoke', ...fields }, SALT)).not.toBe(
        createHmac('sha256', Buffer.from(SALT, 'hex')).update(Buffer.from('bramble-audit-v1')).digest('hex')
      )
      expect(encodeEvent({ kind: 'revoke', ...fields }).includes(Buffer.from('revoke'))).toBe(true)
    })

    it('rejects ill-formed UTF-16 so distinct strings cannot share one commitment', () => {
      const lone = { ...grant, subject: 'agent-\ud800.acme.eth' }
      const replacement = { ...grant, subject: 'agent-\ufffd.acme.eth' }
      expect(() => commit(lone, SALT)).toThrow('UTF-16')
      expect(() => buildAudit(lone)).toThrow('UTF-16')
      expect(() => commit(replacement, SALT)).not.toThrow()
      expect(commit({ ...grant, subject: 'agent-\ud83d\ude00.acme.eth' }, SALT)).toMatch(/^[0-9a-f]{64}$/)
    })

    it('does not let later mutation of the caller object change the returned opening', () => {
      const event = { ...grant }
      const { message, opening } = buildAudit(event)
      event.subject = 'changed.acme.eth'
      expect(opening.event.kind === 'grant' && opening.event.subject).toBe(grant.subject)
      expect(verifyOpening(message, opening)).toBe(true)
      expect(Object.isFrozen(opening.event)).toBe(true)
    })
  })
})
