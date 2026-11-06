import { decodeFunctionResult, encodeFunctionData, keccak256, parseAbi, toHex } from 'viem'
import { namehash, packetToBytes } from 'viem/ens'
import type { EnsClient } from './client'
import { registryAbi, tailnetRegistry, UNIVERSAL_RESOLVER } from './config'
import { cachedSlowText, readSlowText, rememberSlowText } from './slow-text'

export const MULTICALL3 = '0xcA11bde05977b3631167028862bE2a173976CA11'
const multicallAbi = parseAbi([
  // View annotation permits readContract; the selector is unchanged. Only eth_call.
  'function aggregate3((address target, bool allowFailure, bytes callData)[] calls) view returns ((bool success, bytes returnData)[])',
])
// Match viem 2.56's getEnsText/getEnsResolver through the hackathon UR.
// Empty gateways cannot trigger an off-chain lookup; ccipRead stays disabled.
const resolverAbi = parseAbi([
  'function resolveWithGateways(bytes name, bytes data, string[] gateways) view returns (bytes, address)',
  'function findResolver(bytes name) view returns (address, bytes32, uint256)',
  'function text(bytes32 node, string key) view returns (string)',
])

export async function readDeviceFast(label: string, fullname: string, client: EnsClient) {
  const labelhash = BigInt(keccak256(toHex(label)))
  const meshIP = cachedSlowText(client, fullname, 'mesh-ip')
  const textKeys =
    meshIP === undefined
      ? ['pubkey', 'revoked', 'acl', 'acl-granters', 'mesh-ip']
      : ['pubkey', 'revoked', 'acl', 'acl-granters']
  try {
    const name = toHex(packetToBytes(fullname))
    const textCalls = textKeys.map((key) => ({
      target: UNIVERSAL_RESOLVER,
      allowFailure: false,
      callData: encodeFunctionData({
        abi: resolverAbi,
        functionName: 'resolveWithGateways',
        args: [
          name,
          encodeFunctionData({ abi: resolverAbi, functionName: 'text', args: [namehash(fullname), key] }),
          [],
        ],
      }),
    }))
    // One eth_call at latest. No resolver address or authorization is cached.
    const results = await client.readContract({
      address: MULTICALL3,
      abi: multicallAbi,
      functionName: 'aggregate3',
      args: [
        [
          ...textCalls,
          {
            target: UNIVERSAL_RESOLVER,
            allowFailure: false,
            callData: encodeFunctionData({ abi: resolverAbi, functionName: 'findResolver', args: [name] }),
          },
          {
            target: tailnetRegistry(),
            allowFailure: false,
            callData: encodeFunctionData({ abi: registryAbi, functionName: 'getState', args: [labelhash] }),
          },
        ],
      ],
    })
    if (results.length !== textKeys.length + 2 || results.some((r) => !r.success))
      throw new Error('incomplete multicall')
    const texts = results.slice(0, textKeys.length).map((r) => {
      const [data] = decodeFunctionResult({ abi: resolverAbi, functionName: 'resolveWithGateways', data: r.returnData })
      return data === '0x' ? null : decodeFunctionResult({ abi: resolverAbi, functionName: 'text', data }) || null
    })
    const [resolver] = decodeFunctionResult({
      abi: resolverAbi,
      functionName: 'findResolver',
      data: results[textKeys.length].returnData,
    })
    const state = decodeFunctionResult({
      abi: registryAbi,
      functionName: 'getState',
      data: results[textKeys.length + 1].returnData,
    })
    if (meshIP === undefined) rememberSlowText(client, fullname, 'mesh-ip', texts[4])
    return [texts[0], texts[1], texts[2], texts[3], meshIP ?? texts[4], resolver, state] as const
  } catch {
    // Unsupported Multicall3/UR, a reverted subcall or malformed result:
    // retry fresh individual reads on the SAME Tor client. Never reuse an ACL.
    return Promise.all([
      client.getEnsText({ name: fullname, key: 'pubkey' }),
      client.getEnsText({ name: fullname, key: 'revoked' }),
      client.getEnsText({ name: fullname, key: 'acl' }),
      client.getEnsText({ name: fullname, key: 'acl-granters' }),
      readSlowText(client, fullname, 'mesh-ip'),
      client.getEnsResolver({ name: fullname }),
      client.readContract({
        address: tailnetRegistry(),
        abi: registryAbi,
        functionName: 'getState',
        args: [labelhash],
      }),
    ])
  }
}
