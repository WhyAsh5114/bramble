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
    // enum (see docs/12_SOURCE_NOTES.md) — the fixture registered a 10-year
    // expiry, so this must be active.
    expect(record.status).not.toBe(0)
    expect(BigInt(record.expiry)).toBeGreaterThan(BigInt(Math.floor(Date.now() / 1000)))
  })

  it('resolves a nonexistent label to an unset record rather than throwing', async () => {
    const record = await resolveDevice('does-not-exist-xyz')
    expect(record.pubkey).toBeNull()
  })
})
