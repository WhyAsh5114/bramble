import { afterEach, describe, expect, it, vi } from 'vitest'
import { decodeFunctionData, encodeFunctionResult, parseAbi, type Hex } from 'viem'
import { stopTor } from '../src/tor'
import { startFakeTor } from './helpers/fake-tor'
import fixture from './fixtures/dev-tailnet.json'

process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry

const { createEnsClient, resolveDevice } = await import('../src/ens/client')
const { torFetch } = await import('../src/tor')

// The RPC boundary runs real viem encoding/decoding and the ENS Tor transport.
const abi = parseAbi([
  'function aggregate3((address target, bool allowFailure, bytes callData)[] calls) payable returns ((bool success, bytes returnData)[])',
  'function resolveWithGateways(bytes name, bytes data, string[] gateways) view returns (bytes, address)',
  'function findResolver(bytes name) view returns (address, bytes32, uint256)',
  'function text(bytes32 node, string key) view returns (string)',
  'function getState(uint256 id) view returns (uint8, uint64, address, uint256, uint256)',
])
const resolver = '0x0000000000000000000000000000000000001234'
const zeroHash = `0x${'00'.repeat(32)}` as Hex

async function rpc(latencyMs = 0) {
  const state = {
    text: {
      pubkey: fixture.expectedPubkey,
      revoked: '',
      acl: 'grant',
      'acl-granters': 'granter',
      'mesh-ip': '10.77.0.2/24',
    } as Record<string, string>,
    expiry: 4_000_000_000n,
    failMulticall: false,
    failAll: false,
  }
  function answer(data: Hex): Hex {
    const call = decodeFunctionData({ abi, data })
    switch (call.functionName) {
      case 'aggregate3':
        if (state.failMulticall) throw new Error('multicall unavailable')
        return encodeFunctionResult({
          abi,
          functionName: 'aggregate3',
          result: call.args[0].map((c) => ({ success: true, returnData: answer(c.callData) })),
        })
      case 'resolveWithGateways': {
        const inner = decodeFunctionData({ abi, data: call.args[1] })
        if (inner.functionName !== 'text') throw new Error('expected text')
        return encodeFunctionResult({
          abi,
          functionName: 'resolveWithGateways',
          result: [
            encodeFunctionResult({ abi, functionName: 'text', result: state.text[inner.args[1]] ?? '' }),
            resolver,
          ],
        })
      }
      case 'findResolver':
        return encodeFunctionResult({ abi, functionName: 'findResolver', result: [resolver, zeroHash, 0n] })
      case 'getState':
        return encodeFunctionResult({ abi, functionName: 'getState', result: [1, state.expiry, resolver, 42n, 0n] })
      default:
        throw new Error('unexpected call')
    }
  }
  const tor = await startFakeTor(async (_url, init) => {
    if (latencyMs) await new Promise((resolve) => setTimeout(resolve, latencyMs))
    const body = JSON.parse(new TextDecoder().decode(init.body as Uint8Array))
    try {
      if (state.failAll) throw new Error('RPC unavailable')
      const result = answer(body.params[0].data)
      return Response.json({ jsonrpc: '2.0', id: body.id, result })
    } catch {
      return Response.json({ jsonrpc: '2.0', id: body.id, error: { code: -32000, message: 'execution reverted' } })
    }
  })
  const client = createEnsClient(torFetch('ens'))
  return { state, tor, client }
}

afterEach(() => {
  stopTor()
  vi.unstubAllEnvs()
  vi.restoreAllMocks()
})

describe('flagged device resolution', () => {
  it('resolves all seven records with one ENS-isolated RPC request', async () => {
    vi.stubEnv('BRAMBLE_FAST_RESOLVE', '1')
    const direct = vi.spyOn(globalThis, 'fetch')
    const { client, tor } = await rpc()
    const record = await resolveDevice('worker', client)
    expect(record).toMatchObject({
      pubkey: fixture.expectedPubkey,
      acl: ['grant'],
      aclGranters: ['granter'],
      revoked: false,
      meshIP: '10.77.0.2/24',
      expiry: '4000000000',
      tokenId: '42',
      resolverAddress: resolver,
    })
    expect(tor.calls).toHaveLength(1)
    expect(tor.calls[0].isolationKey).toMatch(/^ens:/)
    expect(direct).not.toHaveBeenCalled()
  })

  it('reuses only mesh IP for 60 seconds and observes ACL, revocation, key removal and expiry immediately', async () => {
    vi.stubEnv('BRAMBLE_FAST_RESOLVE', '1')
    let now = 1_000_000
    vi.spyOn(Date, 'now').mockImplementation(() => now)
    const { client, state } = await rpc()
    await resolveDevice('worker', client)
    state.text['mesh-ip'] = '10.77.0.9/24'
    state.text.acl = ''
    state.text['acl-granters'] = ''
    state.text.revoked = 'true'
    state.text.pubkey = ''
    state.expiry = 1n
    const revoked = await resolveDevice('worker', client)
    expect(revoked).toMatchObject({
      meshIP: '10.77.0.2/24',
      acl: [],
      aclGranters: [],
      revoked: true,
      pubkey: null,
      expiry: '1',
    })
    now += 60_001
    expect((await resolveDevice('worker', client)).meshIP).toBe('10.77.0.9/24')
  })

  it('keeps seven individual reads and no routing cache when the flag is off', async () => {
    vi.stubEnv('BRAMBLE_FAST_RESOLVE', '')
    const { client, tor, state } = await rpc()
    await resolveDevice('worker', client)
    state.text['mesh-ip'] = '10.77.0.9/24'
    expect((await resolveDevice('worker', client)).meshIP).toBe('10.77.0.9/24')
    expect(tor.calls).toHaveLength(14)
  })

  it('falls back through ENS Tor when multicall fails, without reusing a grant', async () => {
    vi.stubEnv('BRAMBLE_FAST_RESOLVE', '1')
    const direct = vi.spyOn(globalThis, 'fetch')
    const { client, tor, state } = await rpc()
    await resolveDevice('worker', client)
    state.failMulticall = true
    state.text.acl = ''
    state.text.revoked = 'true'
    expect(await resolveDevice('worker', client)).toMatchObject({ acl: [], revoked: true })
    expect(tor.calls.every((c) => /^ens:/.test(c.isolationKey ?? ''))).toBe(true)
    expect(direct).not.toHaveBeenCalled()
  })

  it('does not share routing metadata between nodes', async () => {
    vi.stubEnv('BRAMBLE_FAST_RESOLVE', '1')
    const { client, state } = await rpc()
    await resolveDevice('worker', client)
    state.text['mesh-ip'] = '10.77.0.9/24'
    const otherNode = createEnsClient(torFetch('ens'))
    expect((await resolveDevice('worker', otherNode)).meshIP).toBe('10.77.0.9/24')
  })

  it('rejects when fresh authorization is unavailable even with a warm routing cache', async () => {
    vi.stubEnv('BRAMBLE_FAST_RESOLVE', '1')
    const { client, state } = await rpc()
    await resolveDevice('worker', client)
    state.failAll = true
    await expect(resolveDevice('worker', client)).rejects.toThrow()
  })

  it.skipIf(process.env.BRAMBLE_RESOLVE_BENCH !== '1')(
    'reports warm fixture timings (50 ms per RPC; not cloud acceptance)',
    async () => {
      for (const flag of ['', '1']) {
        vi.stubEnv('BRAMBLE_FAST_RESOLVE', flag)
        const { client, tor } = await rpc(50)
        await resolveDevice('worker', client)
        for (let sample = 1; sample <= 2; sample++) {
          const beforeCalls = tor.calls.length
          const start = performance.now()
          expect((await resolveDevice('worker', client)).acl).toEqual(['grant'])
          console.info(
            JSON.stringify({
              flag: flag || 'off',
              sample,
              ms: +(performance.now() - start).toFixed(1),
              rpcCalls: tor.calls.length - beforeCalls,
            })
          )
        }
        stopTor()
      }
    }
  )
})
