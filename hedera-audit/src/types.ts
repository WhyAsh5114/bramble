export const AUDIT_VERSION = 1

export type AuditKind = 'grant' | 'revoke' | 'payment'

export interface GrantEvent {
  kind: 'grant'
  /** ENS name of the agent or device that receives the grant. */
  subject: string
  /** 0x-prefixed ACL digest of the service (ADR 0005), never a plaintext service name. */
  serviceDigest: string
  /** Expiry as decimal Unix seconds. */
  expiresAt: string
  /** Hash of the Sepolia transaction that wrote the grant. */
  txRef: string
}

export interface RevokeEvent {
  kind: 'revoke'
  subject: string
  serviceDigest: string
  /** Hash of the Sepolia transaction that removed the grant. */
  txRef: string
}

export interface PaymentEvent {
  kind: 'payment'
  /** Hedera account ID of the payer, for example `0.0.1234`. */
  payer: string
  /** Hedera account ID of the payee. */
  payee: string
  /** Hedera token ID of the asset. */
  asset: string
  /** Amount in atomic units, as a decimal string. */
  amountAtomic: string
  /** Hedera transaction ID of the payment. */
  txRef: string
}

export type AuditEvent = GrantEvent | RevokeEvent | PaymentEvent

/** What the operator keeps so a verifier can later open a commitment. Never post it. */
export interface AuditOpening {
  v: typeof AUDIT_VERSION
  event: AuditEvent
  /** 32 random bytes, lowercase hex. */
  salt: string
}

/** The bytes posted to the HCS topic, as JSON. */
export interface AuditMessage {
  v: typeof AUDIT_VERSION
  kind: AuditKind
  /** HMAC-SHA256 commitment, 64 lowercase hex characters. */
  c: string
}

export interface SubmitReceipt {
  topicId: string
  transactionId: string
  sequenceNumber: string
}

export interface AuditRecord {
  message: AuditMessage
  opening: AuditOpening
  receipt: SubmitReceipt
}

/** The small slice of Hedera that the audit trail needs. Inject a fake in tests. */
export interface TopicClient {
  createTopic(params: { memo: string }): Promise<{ topicId: string }>
  submitMessage(params: { topicId: string; message: Uint8Array }): Promise<{
    transactionId: string
    sequenceNumber: string
  }>
}
