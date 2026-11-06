import { createHmac, randomBytes as nodeRandomBytes, timingSafeEqual } from 'node:crypto'
import { AUDIT_VERSION, type AuditEvent, type AuditKind, type AuditMessage, type AuditOpening } from './types.ts'

export const SALT_BYTES = 32
export const MAX_MESSAGE_BYTES = 1024

const DOMAIN = 'bramble-audit-v1'
const KINDS: readonly AuditKind[] = ['grant', 'revoke', 'payment']
const REQUIRED_FIELDS: Record<AuditKind, readonly string[]> = {
  grant: ['subject', 'serviceDigest', 'expiresAt', 'txRef'],
  revoke: ['subject', 'serviceDigest', 'txRef'],
  payment: ['payer', 'payee', 'asset', 'amountAtomic', 'txRef'],
}
const HEX_64 = /^[0-9a-f]{64}$/

export type RandomBytes = (size: number) => Uint8Array

const LONE_SURROGATE = /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/

/**
 * Copies the schema fields of an event into a fresh frozen object. Only own enumerable string
 * properties count, so inherited or non-enumerable fields cannot pass validation yet be left out
 * of the commitment, and later mutation of the caller's object cannot change the opening.
 */
export function snapshotEvent(event: AuditEvent): AuditEvent {
  if (typeof event !== 'object' || event === null) throw new Error('audit event must be an object')
  const source = event as unknown as Record<string, unknown>
  const own = (name: string): unknown => {
    const descriptor = Object.getOwnPropertyDescriptor(source, name)
    return descriptor && descriptor.enumerable ? descriptor.value : undefined
  }
  const kind = own('kind')
  if (typeof kind !== 'string' || !KINDS.includes(kind as AuditKind))
    throw new Error(`unknown audit kind: ${String(kind)}`)
  const required = REQUIRED_FIELDS[kind as AuditKind]
  for (const name of Object.keys(source)) {
    if (name !== 'kind' && !required.includes(name)) throw new Error(`unexpected field ${name} on ${kind} event`)
  }
  const copy: Record<string, string> = { kind }
  for (const name of required) {
    const value = own(name)
    if (typeof value !== 'string' || value === '') throw new Error(`${kind} event needs a non-empty ${name}`)
    if (LONE_SURROGATE.test(value)) throw new Error(`${kind} event field ${name} is not well-formed UTF-16`)
    copy[name] = value
  }
  return Object.freeze(copy) as unknown as AuditEvent
}

function lengthPrefixed(text: string): Buffer {
  const body = Buffer.from(text, 'utf8')
  const prefix = Buffer.alloc(4)
  prefix.writeUInt32BE(body.length)
  return Buffer.concat([prefix, body])
}

function encodeSnapshot(snapshot: AuditEvent): Buffer {
  const fields = snapshot as unknown as Record<string, string>
  const parts = [lengthPrefixed(DOMAIN)]
  for (const name of Object.keys(fields).sort()) {
    parts.push(lengthPrefixed(name), lengthPrefixed(fields[name]))
  }
  return Buffer.concat(parts)
}

/** Unambiguous encoding: domain tag, then every (name, value) pair sorted by name, each length-prefixed. */
export function encodeEvent(event: AuditEvent): Buffer {
  return encodeSnapshot(snapshotEvent(event))
}

export function newSalt(randomBytes: RandomBytes = nodeRandomBytes): string {
  const salt = randomBytes(SALT_BYTES)
  if (salt.length !== SALT_BYTES) throw new Error(`salt must be ${SALT_BYTES} bytes`)
  return Buffer.from(salt).toString('hex')
}

function commitSnapshot(snapshot: AuditEvent, saltHex: string): string {
  if (!HEX_64.test(saltHex)) throw new Error('salt must be 64 lowercase hex characters')
  return createHmac('sha256', Buffer.from(saltHex, 'hex')).update(encodeSnapshot(snapshot)).digest('hex')
}

export function commit(event: AuditEvent, saltHex: string): string {
  return commitSnapshot(snapshotEvent(event), saltHex)
}

export function buildAudit(
  event: AuditEvent,
  randomBytes?: RandomBytes
): { message: AuditMessage; opening: AuditOpening } {
  const snapshot = snapshotEvent(event)
  const salt = newSalt(randomBytes)
  return {
    message: { v: AUDIT_VERSION, kind: snapshot.kind, c: commitSnapshot(snapshot, salt) },
    opening: { v: AUDIT_VERSION, event: snapshot, salt },
  }
}

export function encodeMessage(message: AuditMessage): Uint8Array {
  const bytes = new TextEncoder().encode(JSON.stringify({ v: message.v, kind: message.kind, c: message.c }))
  if (bytes.length > MAX_MESSAGE_BYTES) throw new Error('audit message exceeds the HCS single-message limit')
  return bytes
}

/** Returns the message, or null for anything that is not a well-formed v1 audit message. */
export function parseMessage(bytes: Uint8Array): AuditMessage | null {
  let value: unknown
  try {
    value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes))
  } catch {
    return null
  }
  if (typeof value !== 'object' || value === null) return null
  const { v, kind, c, ...rest } = value as Record<string, unknown>
  if (Object.keys(rest).length > 0) return null
  if (v !== AUDIT_VERSION) return null
  if (typeof kind !== 'string' || !KINDS.includes(kind as AuditKind)) return null
  if (typeof c !== 'string' || !HEX_64.test(c)) return null
  return { v, kind: kind as AuditKind, c }
}

/** True when the opening reproduces the posted commitment. */
export function verifyOpening(message: AuditMessage, opening: AuditOpening): boolean {
  if (opening.v !== AUDIT_VERSION || message.v !== AUDIT_VERSION) return false
  if (opening.event.kind !== message.kind) return false
  let expected: string
  try {
    expected = commit(opening.event, opening.salt)
  } catch {
    return false
  }
  return timingSafeEqual(Buffer.from(expected, 'hex'), Buffer.from(message.c, 'hex'))
}
