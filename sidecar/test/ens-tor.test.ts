import { afterEach, describe, expect, it, vi } from 'vitest'
import fixture from './fixtures/dev-tailnet.json'
import { stopTor } from '../src/tor'
import { startFakeTor } from './helpers/fake-tor'

process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry

const { RPC_URL } = await import('../src/ens/config')
const { publicClient } = await import('../src/ens/client')

const blockNumberReply = async () =>
  new Response(JSON.stringify({ jsonrpc: '2.0', id: 1, result: '0x10' }), {
    headers: { 'content-type': 'application/json' },
  })

afterEach(() => {
  stopTor()
  vi.restoreAllMocks()
})

describe('ENS reads go through Tor', () => {
  it("sends the RPC request through torFetch('ens') and never through the global fetch", async () => {
    const direct = vi.spyOn(globalThis, 'fetch')
    const tor = await startFakeTor(blockNumberReply)

    expect(await publicClient.getBlockNumber()).toBe(16n)

    expect(tor.calls).toHaveLength(1)
    expect(tor.calls[0].url).toBe(new URL(RPC_URL).href)
    expect(tor.calls[0].isolationKey).toMatch(/^ens:/)
    expect(JSON.parse(String(new TextDecoder().decode(tor.calls[0].init.body as Uint8Array))).method).toBe(
      'eth_blockNumber'
    )
    expect(direct).not.toHaveBeenCalled()
  })

  it('fails closed while Tor is starting instead of falling back to a direct connection', async () => {
    const direct = vi.spyOn(globalThis, 'fetch')

    const read = publicClient.request({ method: 'eth_blockNumber' }, { retryCount: 0 })

    await expect(read).rejects.toThrow(/starting Tor/)
    expect(direct).not.toHaveBeenCalled()
  })

  it('does not follow CCIP-Read gateways, whose lookups viem sends through the global fetch', () => {
    expect(publicClient.ccipRead).toBe(false)
  })
})
