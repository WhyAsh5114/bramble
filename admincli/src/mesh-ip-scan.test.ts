import { describe, expect, it } from 'bun:test'
import fixture from '../../sidecar/test/fixtures/dev-tailnet.json'
import { fixtureClient, labelRegisteredLog } from '../../sidecar/test/fixtures/registry-logs'
import { getTorStatus } from '../../sidecar/src/tor'

const DEPLOY_BLOCK = 11_673_875n
process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry
process.env.BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK = DEPLOY_BLOCK.toString()

const { usedMeshIPs } = await import('./mesh-ip-scan')
const registry = fixture.tailnetRegistry as `0x${string}`

describe('usedMeshIPs (enroll.ts step 4)', () => {
  it('scans through the administrator client with Tor never started', async () => {
    expect(getTorStatus().state).not.toBe('ready')
    const { client, calls } = fixtureClient({
      head: DEPLOY_BLOCK + 10n,
      logs: [
        labelRegisteredLog(registry, 'device1', DEPLOY_BLOCK),
        labelRegisteredLog(registry, 'device2', DEPLOY_BLOCK + 1n),
        labelRegisteredLog(registry, 'new-device', DEPLOY_BLOCK + 2n),
      ],
    })
    const read: string[] = []

    const used = await usedMeshIPs(client, 'new-device', async (_c, label) => {
      read.push(label)
      return `10.77.0.${read.length + 1}/24`
    })

    expect(calls.length).toBeGreaterThan(0)
    expect(read).toEqual(['device1', 'device2'])
    expect(used).toEqual(['10.77.0.2/24', '10.77.0.3/24'])
  })

  it('propagates a scan error rather than allocating from a partial list', async () => {
    const { client } = fixtureClient({
      head: DEPLOY_BLOCK + 10n,
      logs: [],
      failWith: { code: 4444, message: 'pruned history unavailable' },
    })
    await expect(usedMeshIPs(client, 'x', async () => null)).rejects.toThrow()
  })
})
