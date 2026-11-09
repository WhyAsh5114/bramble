import { LabelRegisteredError, type EnrollProgress } from '../../../admincli/src/lib'
import { ApiError, type Clock, type JoinChain, type JoinInput, type JoinRecord } from './contracts'

/** In-memory suggestions. Only confirmed chain writes change membership. */
function hiddenLabelPrefixes(): string[] {
  return (process.env.APPROVAL_HIDE_LABEL_PREFIXES ?? '')
    .split(',')
    .map((prefix) => prefix.trim())
    .filter(Boolean)
}

export class JoinService {
  private readonly rows = new Map<string, JoinRecord>()
  private readonly progress = new Map<string, EnrollProgress>()
  private writing = false
  private enrollmentId?: string
  private attempts: number[] = []

  constructor(
    private readonly chain: JoinChain | undefined,
    private readonly tailnet: string | undefined,
    private readonly clock: Clock
  ) {}

  limit() {
    const now = this.clock.now()
    this.attempts = this.attempts.filter((at) => at > now - 60_000)
    if (this.attempts.length >= 30) {
      throw new ApiError(429, 'rate_limited', 'Join requests are limited to 30 per minute.', { 'retry-after': '60' })
    }
    this.attempts.push(now)
  }

  create(input: JoinInput) {
    if (!this.tailnet) throw new ApiError(503, 'tailnet_not_configured', 'The join tailnet is not configured.')
    if (input.tailnet !== this.tailnet)
      throw new ApiError(400, 'wrong_tailnet', 'This service manages another tailnet.')
    if (this.rows.size >= 1000) throw new ApiError(503, 'queue_full', 'The in-memory join queue is full.')
    const row: JoinRecord = { ...input, id: crypto.randomUUID(), status: 'pending', createdAt: this.now() }
    this.rows.set(row.id, row)
    return { id: row.id, status: row.status }
  }

  list() {
    return [...this.rows.values()].map((row) => structuredClone(row))
  }

  get(id: string) {
    return structuredClone(this.row(id))
  }

  deny(id: string) {
    const row = this.row(id)
    this.pending(row)
    // A partially enrolled identity must be completed and revoked, never called denied.
    if (this.progress.has(id))
      throw new ApiError(409, 'enrollment_started', 'Retry approval before revoking this device.')
    row.status = 'denied'
    row.decidedAt = this.now()
    return this.get(id)
  }

  async approve(id: string) {
    const row = this.row(id)
    this.pending(row)
    const chain = this.requireChain()
    if (this.enrollmentId && this.enrollmentId !== id) {
      throw new ApiError(
        409,
        'enrollment_incomplete',
        'Retry the incomplete enrollment before approving another device.'
      )
    }
    this.lock()
    row.inProgress = 'approving'
    try {
      if (!this.progress.has(id) && (await chain.labelTaken(row.label))) {
        throw new LabelRegisteredError(row.label)
      }
      const progress = this.progress.get(id) ?? { txHashes: {} }
      this.enrollmentId = id
      this.progress.set(id, progress)
      const outcome = await chain.enroll(row, progress)
      row.meshIp = outcome.meshIp
      row.meshCidr = outcome.meshCidr
      row.enrollTransactions = { ...outcome.txHashes }
      row.status = 'approved'
      row.decidedAt = this.now()
      this.enrollmentId = undefined
      delete row.failure
    } catch (error) {
      if (error instanceof LabelRegisteredError) {
        this.enrollmentId = undefined
        this.progress.delete(id)
        throw new ApiError(409, 'label_taken', 'This device label is already registered.')
      }
      row.failure = 'Enrollment is incomplete. Retry approval to resume.'
      throw new ApiError(502, 'enroll_failed', row.failure)
    } finally {
      delete row.inProgress
      this.writing = false
    }
    return this.get(id)
  }

  async revoke(id: string) {
    // Chain-discovered members remain revocable after a service restart.
    if (!this.rows.has(id) && id.startsWith('member:')) {
      const member = (await this.members()).find((row) => row.id === id)
      if (member && !this.rows.has(id)) this.rows.set(id, member)
    }
    const row = this.row(id)
    if (row.status !== 'approved')
      throw new ApiError(409, 'join_not_approved', 'Only an approved device can be revoked.')
    const chain = this.requireChain()
    this.lock()
    row.inProgress = 'revoking'
    try {
      row.revokeTransaction = await chain.revokeDevice(row.label)
      row.status = 'revoked'
      row.revokedAt = this.now()
      row.decidedAt = row.revokedAt
      delete row.failure
    } catch {
      row.failure = 'Device revocation did not confirm. Retry revoke.'
      throw new ApiError(502, 'revoke_failed', row.failure)
    } finally {
      delete row.inProgress
      this.writing = false
    }
    return this.get(id)
  }

  /** Throws unless label is a current, unrevoked member on chain. */
  async requireMember(label: string) {
    const facts = await this.requireChain().members()
    const member = facts.find((item) => item.label === label)
    if (!member || member.revoked || member.expiry <= this.clock.now() / 1000) {
      throw new ApiError(403, 'not_a_member', 'Only an approved device can request access.')
    }
  }

  async members(): Promise<JoinRecord[]> {
    const chain = this.requireChain()
    try {
      const facts = await chain.members()
      return (
        facts
          .filter((member) => member.expiry > this.clock.now() / 1000)
          // Demo hygiene: APPROVAL_HIDE_LABEL_PREFIXES="dryrun-" keeps rehearsal devices out of the console.
          .filter((member) => !hiddenLabelPrefixes().some((prefix) => member.label.startsWith(prefix)))
          .map((member) => {
            const row = [...this.rows.values()].find(
              (row) =>
                row.label === member.label &&
                (row.status === 'approved' || row.status === 'revoked' || this.progress.has(row.id))
            )
            return {
              ...(row
                ? structuredClone(row)
                : {
                    id: `member:${member.label}`,
                    label: member.label,
                    tailnet: this.tailnet!,
                    createdAt: '',
                    pubkey: member.pubkey ?? '',
                  }),
              status:
                member.revoked || row?.revokeTransaction
                  ? 'revoked'
                  : row?.status === 'pending'
                    ? 'pending'
                    : 'approved',
              ...(member.meshIp ? { meshIp: member.meshIp.split('/')[0] } : {}),
            }
          })
      )
    } catch (error) {
      if (error instanceof ApiError) throw error
      throw new ApiError(502, 'members_unavailable', 'ENS membership is temporarily unavailable.')
    }
  }

  private requireChain() {
    if (!this.chain) throw new ApiError(503, 'signer_not_configured', 'The enrollment signer is not configured.')
    return this.chain
  }

  private lock() {
    if (this.writing) throw new ApiError(409, 'operation_in_progress', 'Another enrollment or revocation is running.')
    this.writing = true
  }

  private row(id: string) {
    const row = this.rows.get(id)
    if (!row) throw new ApiError(404, 'join_not_found', 'Join request not found.')
    return row
  }

  private pending(row: JoinRecord) {
    if (row.inProgress) throw new ApiError(409, 'operation_in_progress', 'A device write is running.')
    if (row.status !== 'pending') throw new ApiError(409, 'join_not_pending', 'This join request is no longer pending.')
  }

  private now() {
    return new Date(this.clock.now()).toISOString()
  }
}
