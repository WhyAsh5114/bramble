export interface RequestInput {
  agentLabel: string
  gatewayLabel: string
  service: string
  note?: string
  /** Seconds after confirmed approval; implemented by a server-side revoke timer. */
  requestedDuration?: number
}

export interface IdentityFacts {
  label: string
  pubkeyPublished: boolean
  revoked: boolean
  /** Unix seconds, from ENS. */
  expiry: number
}

export interface IdentityResolver {
  resolveIdentity(label: string): Promise<IdentityFacts | null>
  /** Fresh ACL read; true if the agent has any grant. */
  hasGrant(agentLabel: string): Promise<boolean>
}

export interface TransactionReceipt {
  transactionHash: string
}

/** Implementations return only after confirmation and reconcile ambiguous write failures. */
export interface GrantSigner {
  grant(input: Pick<RequestInput, 'agentLabel' | 'gatewayLabel' | 'service'>): Promise<TransactionReceipt>
  /** Clear this agent's ACL grants. Device enrollment remains unchanged. */
  revoke(agentLabel: string): Promise<TransactionReceipt>
}

export interface Clock {
  now(): number
  schedule(action: () => Promise<void>, delayMs: number): () => void
}

export interface AccessRequest extends RequestInput {
  id: string
  status: 'pending' | 'approving' | 'approved' | 'denied' | 'revoking' | 'revoked'
  createdAt: string
  decidedAt?: string
  grantTransaction?: TransactionReceipt
  revokeTransaction?: TransactionReceipt
  expiresAt?: string
  revokedAt?: string
  revokeReason?: 'manual' | 'duration' | 'external'
  expiryStatus: 'none' | 'scheduled' | 'revoking' | 'failed' | 'revoked'
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string
  ) {
    super(message)
  }
}
