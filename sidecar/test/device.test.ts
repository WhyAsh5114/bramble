import { describe, expect, it } from 'vitest'
import fixture from './fixtures/dev-tailnet.json'

process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry

const { resolveDevice } = await import('../src/ens/client')

describe('resolveDevice', () => {
  it('reads back the provisioned device subname from Sepolia, read-only', async () => {
    const record = await resolveDevice(fixture.deviceLabel)

    expect(record.fullname).toBe(fixture.deviceFullname)
    expect(record.pubkey).toBe(fixture.expectedPubkey)
    // status 0 = expired/unset, non-zero = active in this registry's state
    // enum (see docs/11_SOURCE_NOTES.md) — the fixture registered a 10-year
    // expiry, so this must be active.
    expect(record.status).not.toBe(0)
    expect(BigInt(record.expiry)).toBeGreaterThan(BigInt(Math.floor(Date.now() / 1000)))
    // No `revoked` record has ever been written for this fixture device —
    // must read as false, not throw or come back true on an absent record.
    expect(record.revoked).toBe(false)
    // No `acl` record has ever been written for this fixture device either —
    // must parse to [], not throw or come back with a stray empty-string
    // entry from splitting an absent/empty value.
    expect(record.acl).toEqual([])
    // Same for `acl-granters` — this fixture device has never been
    // configured as a gateway for anything.
    expect(record.aclGranters).toEqual([])
  })

  it('resolves a nonexistent label to an unset record rather than throwing', async () => {
    const record = await resolveDevice('does-not-exist-xyz')
    expect(record.pubkey).toBeNull()
  })
})
