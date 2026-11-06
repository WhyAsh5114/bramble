import { randomUUID } from 'node:crypto'
import {
  ApiError,
  UnresolvedWriteError,
  type AccessRequest,
  type SettleOutcome,
  type Clock,
  type RequestInput,
  type GrantSigner,
  type IdentityResolver,
  type PaymentInput,
} from './contracts'
import type { AuditRecorder } from './audit'

// Adapter errors can echo credentials in messages, shortMessage, or even name.
// Journal only a fixed diagnostic; the HTTP response also stays generic.
function logSignerFailure(operation: 'grant' | 'revoke'): void {
  console.error(`approval: signer ${operation} failed; check signer configuration and upstream availability`)
}

export const systemClock: Clock = {
  now: () => Date.now(),
  schedule(action, delayMs) {
    const timer = setTimeout(() => void action(), delayMs)
    timer.unref()
    return () => clearTimeout(timer)
  },
}

const SETTLE_POLL_MS = 5000

interface UnresolvedEntry {
  error: UnresolvedWriteError
  recordId?: string
  reason: 'manual' | 'duration'
}

export class ApprovalService {
  private readonly requests = new Map<string, AccessRequest>()

  private readonly timers = new Map<string, () => void>()

  private readonly busyAgents = new Set<string>()

  // Submitted writes whose outcome is unknown, by agent. They are re-checked, never resubmitted.
  private readonly unresolved = new Map<string, UnresolvedEntry>()

  constructor(
    private readonly signer?: GrantSigner,
    private readonly resolver?: IdentityResolver,
    private readonly clock: Clock = systemClock,
    private readonly audit?: AuditRecorder,
    /** Parent ENS name of agent labels, for example `acme.eth`. Without it the label is the subject. */
    private readonly ensParent?: string
  ) {}

  /** ADR 0005 digests from the signer. Kept off the record so the HTTP view never carries them. */
  private readonly digests = new Map<string, string>()

  private record(id: string) {
    const record = this.requests.get(id)
    if (!record) throw new ApiError(404, 'request_not_found', 'Request not found.')
    return record
  }

  private pending(id: string) {
    const record = this.record(id)
    if (record.status !== 'pending') throw new ApiError(409, 'request_not_pending', 'Request is no longer pending.')
    return record
  }

  private async exclusive<T>(agentLabel: string, action: () => Promise<T>): Promise<T> {
    if (this.busyAgents.has(agentLabel))
      throw new ApiError(409, 'operation_in_progress', 'An agent write is in progress.')
    this.busyAgents.add(agentLabel)
    try {
      return await action()
    } finally {
      this.busyAgents.delete(agentLabel)
      this.audit?.flush(agentLabel)
    }
  }

  private assertSettled(agentLabel: string) {
    if (this.unresolved.has(agentLabel)) {
      throw new ApiError(
        409,
        'write_unresolved',
        'A submitted transaction for this agent is not confirmed yet; wait for it to settle.'
      )
    }
  }

  private trackUnresolved(agentLabel: string, entry: UnresolvedEntry) {
    this.unresolved.set(agentLabel, entry)
    this.clock.schedule(() => this.settleUnresolved(agentLabel), SETTLE_POLL_MS)
  }

  private async settleUnresolved(agentLabel: string) {
    const entry = this.unresolved.get(agentLabel)
    if (!entry) return
    let outcome: Awaited<ReturnType<UnresolvedWriteError['settle']>>
    try {
      outcome = await entry.error.settle()
    } catch {
      outcome = 'unresolved'
    }
    if (outcome === 'unresolved') {
      this.clock.schedule(() => this.settleUnresolved(agentLabel), SETTLE_POLL_MS)
      return
    }
    this.unresolved.delete(agentLabel)
    this.applySettled(entry, outcome)
    this.audit?.flush(agentLabel)
  }

  private applySettled(entry: UnresolvedEntry, outcome: Exclude<SettleOutcome, 'unresolved'>) {
    const record = entry.recordId ? this.requests.get(entry.recordId) : undefined
    if (!record) return
    delete record.unresolvedWrite
    const transaction = { transactionHash: entry.error.transactionHash }
    if (entry.error.operation === 'grant') {
      if (outcome === 'confirmed') {
        record.status = 'approved'
        record.grantTransaction = transaction
        const digest = entry.error.serviceDigest
        if (digest) this.digests.set(record.id, digest)
        this.submitGrantAudit(record, digest)
        if (record.expiresAt) {
          record.expiryStatus = 'scheduled'
          this.scheduleExpiry(record, Math.max(0, Date.parse(record.expiresAt) - this.clock.now()))
        }
      } else {
        record.status = 'pending'
        delete record.decidedAt
        delete record.expiresAt
        record.expiryStatus = 'none'
      }
    } else if (outcome === 'confirmed') {
      record.revokeTransaction = transaction
      const audit = this.submitRevokeAudit(record.agentLabel, transaction.transactionHash, record)
      if (audit) record.revokeAuditId = audit.id
      this.markRevoked(record, entry.reason)
    } else {
      record.status = 'approved'
      if (entry.reason === 'duration') {
        record.expiryStatus = 'failed'
        this.scheduleExpiry(record, SETTLE_POLL_MS)
      }
    }
  }

  private adapters() {
    if (!this.signer || !this.resolver) {
      throw new ApiError(503, 'signer_not_configured', 'The signer and identity resolver are not configured.')
    }
    return { signer: this.signer, resolver: this.resolver }
  }

  async approve(id: string) {
    const record = this.pending(id)
    return this.exclusive(record.agentLabel, async () => {
      const { signer, resolver } = this.adapters()
      this.assertSettled(record.agentLabel)
      if (await resolver.hasGrant(record.agentLabel)) {
        throw new ApiError(409, 'already_granted', 'Agent already has a grant; revoke it before approving another.')
      }
      // A fresh empty ACL also retires a grant removed by independent admin tooling.
      for (const prior of this.requests.values()) {
        if (prior.agentLabel === record.agentLabel && prior.status === 'approved') this.markRevoked(prior, 'external')
      }
      const identities = await Promise.all([
        resolver.resolveIdentity(record.agentLabel),
        resolver.resolveIdentity(record.gatewayLabel),
      ])
      if (
        identities.some(
          (identity, index) =>
            !identity ||
            identity.label !== [record.agentLabel, record.gatewayLabel][index] ||
            !identity.pubkeyPublished ||
            identity.revoked ||
            identity.expiry * 1000 <= this.clock.now()
        )
      ) {
        throw new ApiError(
          409,
          'identity_not_active',
          'Agent and gateway must have active ENS identities with published public keys.'
        )
      }
      record.status = 'approving'
      let serviceDigest: string | undefined
      try {
        const { serviceDigest: digest, ...transaction } = await signer.grant({
          agentLabel: record.agentLabel,
          gatewayLabel: record.gatewayLabel,
          service: record.service,
        })
        record.grantTransaction = transaction
        serviceDigest = digest
      } catch (error) {
        if (!(error instanceof UnresolvedWriteError)) {
          record.status = 'pending'
          logSignerFailure('grant')
          throw new ApiError(502, 'grant_failed', 'The signer did not confirm the grant.')
        }
        // The grant may land later. Bound its duration from now, the earliest it could be live,
        // and start the revoke timer when it is confirmed.
        record.decidedAt = new Date(this.clock.now()).toISOString()
        if (record.requestedDuration !== undefined) {
          record.expiresAt = new Date(this.clock.now() + record.requestedDuration * 1000).toISOString()
        }
        record.unresolvedWrite = { operation: 'grant', transactionHash: error.transactionHash }
        this.trackUnresolved(record.agentLabel, { error, recordId: record.id, reason: 'manual' })
        return this.view(record)
      }
      record.status = 'approved'
      record.decidedAt = new Date(this.clock.now()).toISOString()
      if (record.requestedDuration !== undefined) {
        record.expiresAt = new Date(this.clock.now() + record.requestedDuration * 1000).toISOString()
        record.expiryStatus = 'scheduled'
        this.scheduleExpiry(record, record.requestedDuration * 1000)
      }
      if (serviceDigest) this.digests.set(record.id, serviceDigest)
      this.submitGrantAudit(record, serviceDigest)
      return this.view(record)
    })
  }

  private submitGrantAudit(record: AccessRequest, serviceDigest?: string) {
    const grantTransaction = record.grantTransaction
    record.grantAuditId = this.audit?.submit(
      record.agentLabel,
      `grant:${record.id}`,
      'grant',
      () =>
        serviceDigest && grantTransaction
          ? {
              kind: 'grant',
              subject: this.subject(record.agentLabel),
              serviceDigest,
              expiresAt: record.expiresAt ? String(Math.floor(Date.parse(record.expiresAt) / 1000)) : '0',
              txRef: grantTransaction.transactionHash,
            }
          : null,
      record.id
    ).id
  }

  private submitRevokeAudit(agentLabel: string, transactionHash: string, record?: AccessRequest) {
    const digest = record ? this.digests.get(record.id) : undefined
    return this.audit?.submit(
      agentLabel,
      `revoke:${record?.id ?? transactionHash}`,
      'revoke',
      () =>
        digest
          ? {
              kind: 'revoke',
              subject: this.subject(agentLabel),
              serviceDigest: digest,
              txRef: transactionHash,
            }
          : null,
      record?.id
    )
  }

  private subject(agentLabel: string) {
    return this.ensParent ? `${agentLabel}.${this.ensParent}` : agentLabel
  }

  /** Posts one commitment for a settled payment. Reporting the same `txRef` again posts nothing. */
  recordPayment(payment: PaymentInput) {
    if (!this.audit) throw new ApiError(503, 'audit_not_configured', 'The audit trail is not configured.')
    const group = `payment:${payment.txRef}`
    const entry = this.audit.submit(group, `payment:${payment.txRef}`, 'payment', () => ({
      kind: 'payment',
      ...payment,
    }))
    this.audit.flush(group)
    return entry
  }

  /** A payer transaction that is not a plain single-payee transfer is shown as a failed row and never committed. It shares the `txRef` key with `recordPayment`, so whichever report arrives first wins. */
  recordUnsupportedPayment(txRef: string) {
    if (!this.audit) return
    const group = `payment:${txRef}`
    this.audit.submit(group, `payment:${txRef}`, 'payment', () => null)
    this.audit.flush(group)
  }

  auditFeed() {
    return this.audit?.list() ?? []
  }

  private scheduleExpiry(record: AccessRequest, delayMs: number) {
    this.timers.get(record.id)?.()
    this.timers.set(
      record.id,
      this.clock.schedule(async () => {
        this.timers.delete(record.id)
        if (record.status === 'revoked') return
        if (record.status === 'revoking') {
          this.scheduleExpiry(record, 5000)
          return
        }
        try {
          await this.revoke(record.agentLabel, 'duration', record.id)
        } catch {
          record.expiryStatus = 'failed'
          this.scheduleExpiry(record, 5000)
        }
      }, delayMs)
    )
  }

  private markRevoked(record: AccessRequest, reason: 'manual' | 'duration' | 'external') {
    this.timers.get(record.id)?.()
    this.timers.delete(record.id)
    record.status = 'revoked'
    record.revokedAt = new Date(this.clock.now()).toISOString()
    record.revokeReason = reason
    if (record.expiresAt) record.expiryStatus = 'revoked'
  }

  deny(id: string) {
    const record = this.pending(id)
    if (this.busyAgents.has(record.agentLabel))
      throw new ApiError(409, 'operation_in_progress', 'An agent write is in progress.')
    record.status = 'denied'
    record.decidedAt = new Date(this.clock.now()).toISOString()
    return this.view(record)
  }

  async revoke(agentLabel: string, reason: 'manual' | 'duration' = 'manual', expectedId?: string) {
    return this.exclusive(agentLabel, async () => {
      const { signer, resolver } = this.adapters()
      this.assertSettled(agentLabel)
      const record = [...this.requests.values()].find(
        (item) => item.agentLabel === agentLabel && item.status === 'approved'
      )
      if (expectedId && record?.id !== expectedId) return
      const requestedAt = new Date(this.clock.now()).toISOString()
      if (!(await resolver.hasGrant(agentLabel))) {
        if (reason === 'duration' && record) {
          this.markRevoked(record, reason)
          return this.view(record)
        }
        throw new ApiError(409, 'not_granted', 'Agent has no grant to revoke.')
      }
      if (record) {
        record.status = 'revoking'
        record.revokeRequestedAt = requestedAt
        if (reason === 'duration') record.expiryStatus = 'revoking'
      }
      let transaction
      try {
        transaction = await signer.revoke(agentLabel)
      } catch (error) {
        if (error instanceof UnresolvedWriteError) {
          const unresolvedWrite = { operation: 'revoke' as const, transactionHash: error.transactionHash }
          this.trackUnresolved(agentLabel, { error, recordId: record?.id, reason })
          if (!record) return { agentLabel, status: 'revoking', unresolvedWrite }
          record.unresolvedWrite = unresolvedWrite
          return this.view(record)
        }
        if (record) {
          record.status = 'approved'
          delete record.revokeRequestedAt
        }
        logSignerFailure('revoke')
        throw new ApiError(502, 'revoke_failed', 'The signer did not confirm the revoke.')
      }
      const entry = this.submitRevokeAudit(agentLabel, transaction.transactionHash, record)
      if (!record) return { agentLabel, status: 'revoked', revokeTransaction: transaction }
      record.revokeTransaction = transaction
      if (entry) record.revokeAuditId = entry.id
      this.markRevoked(record, reason)
      return this.view(record)
    })
  }

  create(input: RequestInput) {
    const record: AccessRequest = {
      ...input,
      id: randomUUID(),
      status: 'pending',
      createdAt: new Date(this.clock.now()).toISOString(),
      expiryStatus: 'none',
    }
    this.requests.set(record.id, record)
    return this.view(record)
  }

  list() {
    return [...this.requests.values()].map((record) => this.view(record))
  }

  get(id: string) {
    return this.view(this.record(id))
  }

  private view(record: AccessRequest) {
    return {
      ...record,
      serverTime: new Date(this.clock.now()).toISOString(),
      durationMode: record.requestedDuration === undefined ? null : 'server-timer',
      remainingSeconds:
        record.expiresAt === undefined
          ? null
          : record.status === 'revoked'
            ? 0
            : Math.max(0, Math.ceil((Date.parse(record.expiresAt) - this.clock.now()) / 1000)),
    }
  }
}
