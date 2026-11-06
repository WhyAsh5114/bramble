// listDeviceLabels scans the tailnet registry's LabelRegistered logs.
//
// The default suite is hermetic: it injects a fixture-backed RPC client
// (./fixtures/registry-logs.ts) and never touches the network. Public Sepolia
// nodes prune old logs and answer this scan with error 4444, `pruned history
// unavailable`, so a live scan cannot gate `pnpm verify`.
//
// The live read-only scan against public Sepolia is opt-in. Run it with:
//
//   BRAMBLE_LIVE_SEPOLIA=1 pnpm --filter @bramble/sidecar test -- devices
//
// It needs an RPC endpoint that still serves the registry's history from its
// deploy block. The public default does not, as of 6 November 2026.
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { stopTor } from '../src/tor'
import { startDirectTor } from './helpers/fake-tor'
import fixture from './fixtures/dev-tailnet.json'
import { fixtureClient, labelRegisteredLog } from './fixtures/registry-logs'

const DEPLOY_BLOCK = 11_673_875n

process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry
process.env.BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK = DEPLOY_BLOCK.toString()

const { listDeviceLabels } = await import('../src/ens/devices')
const { publicClient } = await import('../src/ens/client')

const registry = fixture.tailnetRegistry as `0x${string}`
const otherRegistry = '0x00000000000000000000000000000000000dead0' as const
// Head is the first block of a third window, so the scan must page, and the
// final window is exactly one block. That catches a loop that stops one
// window early when head lands on a window boundary.
const HEAD = DEPLOY_BLOCK + 18_000n

const logs = [
  labelRegisteredLog(registry, 'device1', DEPLOY_BLOCK),
  labelRegisteredLog(registry, 'device2', DEPLOY_BLOCK + 8_999n), // last block of window 1
  labelRegisteredLog(registry, 'device3', DEPLOY_BLOCK + 9_000n), // first block of window 2
  labelRegisteredLog(registry, 'device1', DEPLOY_BLOCK + 12_345n), // re-registration after expiry
  labelRegisteredLog(registry, 'agent1', HEAD), // chain head, alone in the final window
  labelRegisteredLog(otherRegistry, 'not-ours', DEPLOY_BLOCK + 10n),
  labelRegisteredLog(registry, 'too-new', HEAD + 1n), // past head, never requested
]

describe('listDeviceLabels (fixture RPC)', () => {
  it('returns every label registered from the deploy block to head, deduplicated', async () => {
    const { client } = fixtureClient({ head: HEAD, logs })

    const labels = await listDeviceLabels(client)

    expect(labels).toEqual(['device1', 'device2', 'device3', 'agent1'])
  })

  it('pages eth_getLogs in contiguous windows of at most 9,000 blocks, scoped to the registry', async () => {
    const { client, calls } = fixtureClient({ head: HEAD, logs })

    await listDeviceLabels(client)

    expect(calls.map((c) => [c.fromBlock, c.toBlock])).toEqual([
      [DEPLOY_BLOCK, DEPLOY_BLOCK + 8_999n],
      [DEPLOY_BLOCK + 9_000n, DEPLOY_BLOCK + 17_999n],
      [DEPLOY_BLOCK + 18_000n, HEAD],
    ])
    for (const call of calls) {
      expect(call.address.toLowerCase()).toBe(registry.toLowerCase())
      expect(call.topics[0]).toBe(logs[0].topics[0])
    }
  })

  it('rejects on pruned history (4444) instead of returning a partial list', async () => {
    // admincli's enroll.ts allocates a mesh-ip from this list. A partial
    // list could hand out an address another device already holds, so the
    // error must reach the caller.
    const { client } = fixtureClient({
      head: HEAD,
      logs,
      failWith: { code: 4444, message: 'pruned history unavailable' },
    })

    await expect(listDeviceLabels(client)).rejects.toThrow(/pruned history unavailable/)
  })
})

describe.runIf(process.env.BRAMBLE_LIVE_SEPOLIA === '1')('listDeviceLabels (live Sepolia, opt-in)', () => {
  // The sidecar's client only works once Tor is up; direct Tor stands in so
  // the live scan needs no onion circuit.
  beforeAll(startDirectTor)
  afterAll(stopTor)

  it('scans real registered labels on Sepolia, read-only', async () => {
    const labels = await listDeviceLabels(publicClient)
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
