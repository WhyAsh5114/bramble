import { describe, expect, it } from 'bun:test'
import { x25519 } from '@noble/curves/ed25519'
import { bytesToHex } from '@noble/hashes/utils'
import {
  computeGrantDigests,
  grantAcl,
  parseAclRecord,
  parseServiceNames,
  revokeAcl,
  setDeviceRevoked,
  type LibDeps,
  type PendingTx,
  type Resolver,
  type Signer,
} from './lib'
import { aclDigestECDH } from './roles'

const TAILNET = 'mesh.eth'

function newIdentity() {
  const priv = x25519.utils.randomPrivateKey()
  return { privHex: bytesToHex(priv), pubHex: bytesToHex(x25519.getPublicKey(priv)) }
}

// In-memory chain: one text-record map shared by a Resolver and a Signer.
// Like viem's getEnsText, an empty record reads back as null.
function memoryChain(seed: Record<string, string>) {
  const records = new Map(Object.entries(seed))
  const writes: Array<{ name: string; key: string; value: string }> = []
  const resolver: Resolver = {
    async getText(name, key) {
      const v = records.get(`${name}|${key}`)
      return v === undefined || v === '' ? null : v
    },
  }
  let revertNext = false
  const signer: Signer = {
    async setText(name, key, value): Promise<PendingTx> {
      writes.push({ name, key, value })
      const hash = `0x${writes.length.toString(16).padStart(64, '0')}`
      const reverts = revertNext
      revertNext = false
      return {
        hash,
        async wait() {
          if (reverts) throw new Error('setText reverted on-chain')
          records.set(`${name}|${key}`, value)
        },
      }
    },
  }
  return { records, writes, resolver, signer, revertNext: () => (revertNext = true) }
}

function setup() {
  const granter = newIdentity()
  const gateway = newIdentity()
  const device = newIdentity()
  const chain = memoryChain({
    [`granter.${TAILNET}|pubkey`]: granter.pubHex,
    [`gateway.${TAILNET}|pubkey`]: gateway.pubHex,
    [`agent.${TAILNET}|pubkey`]: device.pubHex,
  })
  const lines: string[] = []
  const deps: LibDeps = {
    resolver: chain.resolver,
    signer: chain.signer,
    tailnetName: TAILNET,
    log: (l) => lines.push(l),
  }
  const params = (serviceNames: string[], key = granter.privHex) => ({
    deviceLabel: 'agent',
    gatewayLabel: 'gateway',
    granterLabel: 'granter',
    serviceNames,
    loadGranterKey: async () => key,
  })
  const digest = (service: string) => aclDigestECDH(granter.privHex, gateway.pubHex, device.pubHex, service)
  return { granter, gateway, device, chain, deps, lines, params, digest }
}

describe('parseServiceNames / parseAclRecord', () => {
  it('trims and drops blanks', () => {
    expect(parseServiceNames(' web, ,db ,')).toEqual(['web', 'db'])
    expect(parseAclRecord(null)).toEqual([])
    expect(parseAclRecord('a, b')).toEqual(['a', 'b'])
  })
})

describe('grantAcl then revokeAcl with an in-memory signer', () => {
  it('writes the ECDH digest, then removes it again', async () => {
    const t = setup()
    const granted = await grantAcl(t.deps, t.params(['web']))
    expect(granted.digests).toEqual([t.digest('web')])
    expect(granted.before).toBe('')
    expect(granted.after).toBe(t.digest('web'))
    expect(t.chain.records.get(`agent.${TAILNET}|acl`)).toBe(t.digest('web'))
    expect(granted.txHash).toBeDefined()

    const revoked = await revokeAcl(t.deps, t.params(['web']))
    expect(revoked.before).toBe(t.digest('web'))
    expect(revoked.after).toBe('')
    expect(await t.chain.resolver.getText(`agent.${TAILNET}`, 'acl')).toBeNull()
    expect(t.chain.writes.map((w) => w.key)).toEqual(['acl', 'acl'])
    expect(t.chain.writes[1].value).toBe('')
  })

  it('never puts the plaintext service name on chain', async () => {
    const t = setup()
    await grantAcl(t.deps, t.params(['secret-staging']))
    for (const w of t.chain.writes) expect(w.value).not.toContain('secret-staging')
  })

  it('keeps other grants: granting adds, revoking removes only the named service', async () => {
    const t = setup()
    t.chain.records.set(`agent.${TAILNET}|acl`, 'abc123')
    await grantAcl(t.deps, t.params(['web', 'db']))
    expect(t.chain.records.get(`agent.${TAILNET}|acl`)).toBe(`abc123,${t.digest('web')},${t.digest('db')}`)

    await revokeAcl(t.deps, t.params(['web']))
    expect(t.chain.records.get(`agent.${TAILNET}|acl`)).toBe(`abc123,${t.digest('db')}`)
  })

  it('granting twice is idempotent', async () => {
    const t = setup()
    await grantAcl(t.deps, t.params(['web']))
    await grantAcl(t.deps, t.params(['web']))
    expect(t.chain.records.get(`agent.${TAILNET}|acl`)).toBe(t.digest('web'))
  })

  it('sends no transaction when revoking a grant that is absent', async () => {
    const t = setup()
    t.chain.records.set(`agent.${TAILNET}|acl`, 'abc123')
    const result = await revokeAcl(t.deps, t.params(['web']))
    expect(result.txHash).toBeUndefined()
    expect(result.after).toBe('abc123')
    expect(t.chain.writes).toEqual([])
  })

  it('logs the same progress lines the CLI printed', async () => {
    const t = setup()
    await grantAcl(t.deps, t.params(['web']))
    expect(t.lines[0]).toBe(`granter: granter.${TAILNET}`)
    expect(t.lines[3]).toBe(`services: web -> digests: ${t.digest('web')}`)
    expect(t.lines[4]).toBe(`current acl digests for agent.${TAILNET}: (unset)`)
    expect(t.lines[5]).toMatch(/^ {3}tx: 0x/)
    expect(t.lines[6]).toBe(`done. agent.${TAILNET}'s acl digests are now ${t.digest('web')}.`)
  })

  it('does not write when the transaction reverts', async () => {
    const t = setup()
    t.chain.revertNext()
    await expect(grantAcl(t.deps, t.params(['web']))).rejects.toThrow(/reverted/)
    expect(t.chain.records.has(`agent.${TAILNET}|acl`)).toBe(false)
  })
})

describe('computeGrantDigests guards', () => {
  it("rejects a granter key that does not match the granter's published pubkey", async () => {
    const t = setup()
    const wrong = newIdentity().privHex
    await expect(computeGrantDigests(t.deps, t.params(['web'], wrong))).rejects.toThrow(/doesn't match/)
    await expect(grantAcl(t.deps, t.params(['web'], wrong))).rejects.toThrow(/doesn't match/)
    expect(t.chain.writes).toEqual([])
  })

  it('accepts a 0x-prefixed granter key', async () => {
    const t = setup()
    const out = await computeGrantDigests(t.deps, t.params(['web'], `0x${t.granter.privHex}`))
    expect(out.digests).toEqual([t.digest('web')])
  })

  it('binds the digest to the requesting device', async () => {
    const t = setup()
    const other = newIdentity()
    t.chain.records.set(`other.${TAILNET}|pubkey`, other.pubHex)
    const forAgent = await computeGrantDigests(t.deps, t.params(['web']))
    const forOther = await computeGrantDigests(t.deps, { ...t.params(['web']), deviceLabel: 'other' })
    expect(forOther.digests[0]).not.toBe(forAgent.digests[0])
  })

  it('names the missing record when the gateway or device is not enrolled', async () => {
    const t = setup()
    await expect(computeGrantDigests(t.deps, { ...t.params(['web']), gatewayLabel: 'nope' })).rejects.toThrow(
      /no pubkey found for gateway nope\.mesh\.eth/
    )
    await expect(computeGrantDigests(t.deps, { ...t.params(['web']), deviceLabel: 'nope' })).rejects.toThrow(
      /no pubkey found for device nope\.mesh\.eth/
    )
  })
})

describe('setDeviceRevoked', () => {
  it('sets then clears the revoked record', async () => {
    const t = setup()
    const set = await setDeviceRevoked(t.deps, { deviceLabel: 'agent', value: true })
    expect(set.after).toBe('true')
    expect(t.chain.records.get(`agent.${TAILNET}|revoked`)).toBe('true')
    expect(set.confirmedMs).toBeGreaterThanOrEqual(0)

    const cleared = await setDeviceRevoked(t.deps, { deviceLabel: 'agent', value: false })
    expect(cleared.after).toBe('false')
    expect(t.chain.writes.map((w) => `${w.key}=${w.value}`)).toEqual(['revoked=true', 'revoked=false'])
  })
})
