import { describe, expect, it } from 'bun:test'
import { x25519 } from '@noble/curves/ed25519'
import { bytesToHex } from '@noble/hashes/utils'
import type { LibDeps, PendingTx, Resolver, Signer } from '../../../admincli/src/lib'
import { aclDigestECDH } from '../../../admincli/src/roles'
import { resetDemo, type ResetParams } from './reset'

const TAILNET = 'mesh.eth'
const AGENT = `agent.${TAILNET}`

function newIdentity() {
  const priv = x25519.utils.randomPrivateKey()
  return { privHex: bytesToHex(priv), pubHex: bytesToHex(x25519.getPublicKey(priv)) }
}

// In-memory chain, as in admincli/src/lib.test.ts. An empty record reads as null.
function setup(seed: { acl?: string; revoked?: string; enrolled?: boolean } = {}) {
  const granter = newIdentity()
  const gateway = newIdentity()
  const device = newIdentity()
  const records = new Map<string, string>([
    [`granter.${TAILNET}|pubkey`, granter.pubHex],
    [`gateway.${TAILNET}|pubkey`, gateway.pubHex],
  ])
  if (seed.enrolled !== false) records.set(`${AGENT}|pubkey`, device.pubHex)
  if (seed.acl !== undefined) records.set(`${AGENT}|acl`, seed.acl)
  if (seed.revoked !== undefined) records.set(`${AGENT}|revoked`, seed.revoked)
  const writes: Array<{ name: string; key: string; value: string }> = []
  const resolver: Resolver = {
    async getText(name, key) {
      const v = records.get(`${name}|${key}`)
      return v === undefined || v === '' ? null : v
    },
  }
  const signer: Signer = {
    async setText(name, key, value): Promise<PendingTx> {
      writes.push({ name, key, value })
      return {
        hash: `0x${writes.length.toString(16).padStart(64, '0')}`,
        async wait() {
          records.set(`${name}|${key}`, value)
        },
      }
    },
  }
  const lines: string[] = []
  const deps: LibDeps = { resolver, signer, tailnetName: TAILNET, log: (l) => lines.push(l) }
  let keyLoads = 0
  const params = (over: Partial<ResetParams> = {}): ResetParams => ({
    agentLabel: 'agent',
    gatewayLabel: 'gateway',
    granterLabel: 'granter',
    serviceNames: ['web'],
    loadGranterKey: async () => {
      keyLoads++
      return granter.privHex
    },
    ...over,
  })
  const digest = (service: string) => aclDigestECDH(granter.privHex, gateway.pubHex, device.pubHex, service)
  return { records, writes, deps, lines, params, digest, keyLoads: () => keyLoads }
}

describe('resetDemo', () => {
  it('clears revoked and removes the demo grant', async () => {
    const t = setup({ revoked: 'true' })
    t.records.set(`${AGENT}|acl`, t.digest('web'))

    const result = await resetDemo(t.deps, t.params())

    expect(t.records.get(`${AGENT}|revoked`)).toBe('false')
    expect(t.records.get(`${AGENT}|acl`)).toBe('')
    expect(t.records.get(`${AGENT}|pubkey`)).toBeDefined()
    expect(t.writes.map((w) => w.key)).toEqual(['revoked', 'acl'])
    expect(result.txHashes).toHaveLength(2)
    expect(result.deviceFullname).toBe(AGENT)
  })

  it('is idempotent: the second run sends no transaction and needs no granter key', async () => {
    const t = setup({ revoked: 'true' })
    t.records.set(`${AGENT}|acl`, t.digest('web'))
    await resetDemo(t.deps, t.params())
    const writesAfterFirst = t.writes.length
    const keyLoadsAfterFirst = t.keyLoads()

    const second = await resetDemo(t.deps, t.params())

    expect(t.writes).toHaveLength(writesAfterFirst)
    expect(t.keyLoads()).toBe(keyLoadsAfterFirst)
    expect(second.actions).toEqual([])
    expect(second.txHashes).toEqual([])
  })

  it('does nothing to a device that is already in the initial state', async () => {
    const t = setup({ revoked: 'false' })
    const result = await resetDemo(t.deps, t.params())
    expect(t.writes).toEqual([])
    expect(result.actions).toEqual([])
  })

  it('clears digests the demo did not grant, keeping the device enrolled', async () => {
    const t = setup()
    t.records.set(`${AGENT}|acl`, `abc123,${t.digest('web')}`)

    await resetDemo(t.deps, t.params())

    expect(t.records.get(`${AGENT}|acl`)).toBe('')
    expect(t.writes.map((w) => `${w.key}=${w.value}`)).toEqual([`acl=abc123`, 'acl='])
  })

  it('clears every digest when no service names are given, without loading the granter key', async () => {
    const t = setup()
    t.records.set(`${AGENT}|acl`, 'abc123')

    await resetDemo(t.deps, t.params({ serviceNames: [] }))

    expect(t.records.get(`${AGENT}|acl`)).toBe('')
    expect(t.keyLoads()).toBe(0)
  })

  it('clears a nonempty acl record that holds no digest, without loading the granter key', async () => {
    const t = setup()
    t.records.set(`${AGENT}|acl`, ', ,')

    const result = await resetDemo(t.deps, t.params())

    expect(t.records.get(`${AGENT}|acl`)).toBe('')
    expect(t.writes.map((w) => `${w.key}=${w.value}`)).toEqual(['acl='])
    expect(t.keyLoads()).toBe(0)
    expect(result.actions).toHaveLength(1)
  })

  it('--dry-run reports a digest-less nonempty record as an action', async () => {
    const t = setup()
    t.records.set(`${AGENT}|acl`, ',')
    const result = await resetDemo(t.deps, t.params({ dryRun: true }))
    expect(result.actions).toEqual(['would clear the acl record, which holds no digest'])
    expect(t.writes).toEqual([])
  })

  it('fails when the acl record is still nonempty after the reset', async () => {
    const t = setup()
    t.records.set(`${AGENT}|acl`, ',')
    // A signer whose write is accepted but never lands.
    const lost: typeof t.deps = {
      ...t.deps,
      signer: {
        async setText() {
          return { hash: '0x1', async wait() {} }
        },
      },
    }
    await expect(resetDemo(lost, t.params())).rejects.toThrow(/did not reach the initial state/)
  })

  it('refuses a device that is not enrolled and writes nothing', async () => {
    const t = setup({ enrolled: false, revoked: 'true' })
    await expect(resetDemo(t.deps, t.params())).rejects.toThrow(/not enrolled/)
    expect(t.writes).toEqual([])
  })

  it('fails when the granter key does not match the published pubkey', async () => {
    const t = setup()
    t.records.set(`${AGENT}|acl`, t.digest('web'))
    const wrong = newIdentity()
    await expect(resetDemo(t.deps, t.params({ loadGranterKey: async () => wrong.privHex }))).rejects.toThrow(
      /doesn't match/
    )
    expect(t.writes).toEqual([])
  })

  it('--dry-run reports the actions and sends nothing', async () => {
    const t = setup({ revoked: 'true' })
    t.records.set(`${AGENT}|acl`, t.digest('web'))

    const result = await resetDemo(t.deps, t.params({ dryRun: true }))

    expect(t.writes).toEqual([])
    expect(t.keyLoads()).toBe(0)
    expect(result.actions.length).toBeGreaterThan(0)
    expect(result.actions.every((a) => a.startsWith('would'))).toBe(true)
    expect(result.txHashes).toEqual([])
  })

  it('never logs the granter key or plaintext service names to chain', async () => {
    const t = setup()
    t.records.set(`${AGENT}|acl`, t.digest('secret-staging'))
    await resetDemo(t.deps, t.params({ serviceNames: ['secret-staging'] }))
    for (const w of t.writes) expect(w.value).not.toContain('secret-staging')
  })
})
