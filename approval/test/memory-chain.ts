import type { GrantSigner, IdentityFacts, IdentityResolver, RequestInput } from '../src/lib/contracts'

/** No keys, RPC or transactions: a test-only chain boundary. */
export class MemoryChain implements GrantSigner, IdentityResolver {
  readonly identities = new Map<string, IdentityFacts>(
    ['agent-1', 'gateway-1'].map((label) => [
      label,
      { label, pubkeyPublished: true, revoked: false, expiry: 4_102_444_800 },
    ])
  )
  readonly grants = new Set<string>()
  readonly writes: string[] = []
  failGrant = false
  failRevoke = false
  beforeGrant?: () => Promise<void>

  async resolveIdentity(label: string) {
    return this.identities.get(label) ?? null
  }

  async hasGrant(label: string) {
    return this.grants.has(label)
  }

  async grant(input: Pick<RequestInput, 'agentLabel' | 'gatewayLabel' | 'service'>) {
    await this.beforeGrant?.()
    if (this.failGrant) throw new Error('sensitive adapter diagnostic must never reach the API')
    this.grants.add(input.agentLabel)
    this.writes.push(`grant:${input.agentLabel}`)
    return { transactionHash: `test-grant-${this.writes.length}` }
  }

  async revoke(label: string) {
    if (this.failRevoke) throw new Error('sensitive adapter diagnostic must never reach the API')
    this.grants.delete(label)
    this.writes.push(`revoke:${label}`)
    return { transactionHash: `test-revoke-${this.writes.length}` }
  }
}
