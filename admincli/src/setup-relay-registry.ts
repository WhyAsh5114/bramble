// One-time, tailnet-operator action: deploys a fresh UserRegistry
// subregistry and registers it under this tailnet as "relays" — a
// namespace pointer, not a device. Everything under it (e.g.
// relay1.relays.<tailnet>) lives in a registry contract device/agent
// subnames never touch, satisfying Gate 1.4's structural-separation
// requirement for relay records (docs/05_BUILD_PLAN.md Gate 1.4,
// docs/adr/0003-rendezvous-relay-split.md's Consequence section).
//
// Mirrors scripts/provision-dev-tailnet/index.ts's own
// "deployUserRegistryProxy" step exactly (same VerifiableFactory +
// UserRegistry proxy pattern already proven end to end there) — duplicated
// rather than shared, same as every other cross-package boilerplate in this
// repo (see admincli/src/setup.ts's comment).
//
// Usage: bun run setup-relay-registry.ts
import {
  createWalletClient,
  http,
  encodeAbiParameters,
  encodeFunctionData,
  parseEventLogs,
  parseAbi,
  keccak256,
  stringToHex,
  namehash,
  getAddress,
} from 'viem'
import {
  RPC_URL,
  tailnetName,
  tailnetRegistry,
  VERIFIABLE_FACTORY,
  USER_REGISTRY_IMPL,
  ALL_ROLES,
  REGISTRATION_ROLE_BITMAP,
} from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { registryWriteAbi } from './roles'

const verifiableFactoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
  'event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)',
])
const userRegistryInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants)'])

async function main() {
  const account = accountFromEnv()
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  async function writeAndWait(label: string, args: Parameters<typeof wallet.writeContract>[0]) {
    const hash = await wallet.writeContract(args)
    console.log(`   tx (${label}): ${hash}`)
    const receipt = await publicClient.waitForTransactionReceipt({ hash })
    if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
    return receipt
  }

  console.log('[1/2] Deploying relay UserRegistry proxy...')
  const version = BigInt(Date.now()) * 1000n + BigInt(Math.floor(Math.random() * 1000))
  const salt = BigInt(
    keccak256(
      encodeAbiParameters(
        [{ type: 'bytes32' }, { type: 'bytes32' }, { type: 'uint256' }],
        [keccak256(stringToHex('RelayRegistry')), namehash(`relays.${tailnetName()}`), version]
      )
    )
  )
  const initData = encodeFunctionData({
    abi: userRegistryInitAbi,
    functionName: 'initialize',
    args: [[{ account: account.address, roleBitmap: ALL_ROLES }]],
  })
  const deployReceipt = await writeAndWait('deploy relay registry proxy', {
    address: getAddress(VERIFIABLE_FACTORY),
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [getAddress(USER_REGISTRY_IMPL), salt, initData],
  })
  const [deployLog] = parseEventLogs({
    abi: verifiableFactoryAbi,
    eventName: 'ProxyDeployed',
    logs: deployReceipt.logs,
  })
  const relayRegistryAddress = deployLog.args.proxyAddress
  console.log(`   relay registry: ${relayRegistryAddress}`)
  const deployBlock = deployReceipt.blockNumber

  console.log(`\n[2/2] Registering relays.${tailnetName()} (requires ROLE_REGISTRAR on the tailnet registry)...`)
  const farExpiry = BigInt(Math.floor(Date.now() / 1000) + 10 * 365 * 24 * 60 * 60)
  const zeroAddress = '0x0000000000000000000000000000000000000000' as const
  await writeAndWait('register relays', {
    address: tailnetRegistry(),
    abi: registryWriteAbi,
    functionName: 'register',
    args: ['relays', account.address, relayRegistryAddress, zeroAddress, REGISTRATION_ROLE_BITMAP, farExpiry],
  })

  console.log(
    `\ndone. relays.${tailnetName()} -> relay registry ${relayRegistryAddress} (deploy block ${deployBlock}).`
  )
  console.log('\nAdd to .env:')
  console.log(`BRAMBLE_RELAY_REGISTRY=${relayRegistryAddress}`)
  console.log(`BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK=${deployBlock}`)
}

main().catch((err) => {
  console.error('\nsetup-relay-registry failed:', err)
  process.exit(1)
})
