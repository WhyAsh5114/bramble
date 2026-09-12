import { describe, expect, it } from 'vitest'
import fixture from './fixtures/dev-tailnet.json'

process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry
process.env.BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK = '11673875'

const { listDeviceLabels } = await import('../src/ens/devices')

describe('listDeviceLabels', () => {
  it('scans real registered labels on Sepolia, read-only', async () => {
    const labels = await listDeviceLabels()
    // Not asserting a specific label or exact list — real, growing,
    // continuously-enrolled-into chain state (this dev tailnet gets fresh
    // enrollments regularly), and the public RPC endpoint this repo
    // defaults to is a load-balanced multi-node pool whose backends can
    // lag each other by tens of thousands of blocks, so any one specific
    // recent label is not reliably present on every call. What's provable
    // regardless of which backend answers: the scan reaches the registry
    // and returns real, non-empty, deduplicated data — not an empty list
    // from a silently-broken query, and not a raw log dump with a label
    // repeated once per re-registration (mirrors resolveRelays()'s own
    // uniqueLabels handling).
    expect(labels.length).toBeGreaterThan(0)
    expect(new Set(labels).size).toBe(labels.length)
  })
})
