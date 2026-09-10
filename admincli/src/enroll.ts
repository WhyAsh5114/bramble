// Enrolls a new device subname: deploys a dedicated Permissioned Resolver
// for it (mirrors scripts/gate0.3-eac-check / scripts/provision-dev-tailnet's
// proven pattern — "a subname with its own Permissioned Resolver"), calls
// register() on the tailnet's subregistry, then writes the initial pubkey.
//
// register() requires the caller to hold ROLE_REGISTRAR on the subregistry
// (root-scoped — see docs/adr/0004 and sidecar/src/ens/config.ts's
// ROLE_REGISTRAR comment); the resolver deploy grants ALL_ROLES on the new
// resolver to the deployer (this same caller), so no separate rotate/revoke
// grant is needed just to write the initial pubkey — an operator running
// this command already deployed the resolver they're writing to.
//
// Usage: bun run enroll.ts <device-label> <pubkey> [expiry-years=10]
//
// All three writes below sign through the Ledger (Gate 3.1,
// docs/05_BUILD_PLAN.md) via wallet-cli send — three separate physical
// confirmations, one per transaction, no software fallback.
import {
  type Address,
  encodeAbiParameters,
  encodeFunctionData,
  parseEventLogs,
  parseAbi,
  keccak256,
  stringToHex,
  toHex,
} from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import {
  tailnetName,
  tailnetRegistry,
  VERIFIABLE_FACTORY,
  PERMISSIONED_RESOLVER_IMPL,
  ALL_ROLES,
  REGISTRATION_ROLE_BITMAP,
} from '../../sidecar/src/ens/config'
import { publicClient } from './setup'
import { registryWriteAbi, resolverWriteAbi } from './roles'
import { ledgerAddressFromEnv } from './ledger-address'
import { sendViaDevice } from './ledger-send'

const verifiableFactoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
  'event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)',
])
const resolverInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants, bytes[] calls)'])

async function main() {
  const [deviceLabel, pubkey, expiryYearsArg] = process.argv.slice(2)
  if (!deviceLabel || !pubkey) {
    throw new Error('usage: bun run enroll.ts <device-label> <pubkey> [expiry-years=10]')
  }
  const expiryYears = expiryYearsArg ? Number(expiryYearsArg) : 10

  const callerAddress = ledgerAddressFromEnv()

  async function writeAndWait(
    label: string,
    call: {
      address: Address
      abi: Parameters<typeof encodeFunctionData>[0]['abi']
      functionName: string
      args: readonly unknown[]
    }
  ) {
    const data = encodeFunctionData({ abi: call.abi, functionName: call.functionName, args: call.args } as Parameters<
      typeof encodeFunctionData
    >[0])
    console.log(`   confirm on the Ledger device (${label})...`)
    const hash = await sendViaDevice(call.address, data)
    console.log(`   tx (${label}): ${hash}`)
    const receipt = await publicClient.waitForTransactionReceipt({ hash })
    if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
    return receipt
  }

  console.log(`[1/3] Deploying a dedicated resolver for ${deviceLabel}...`)
  const version = BigInt(Date.now()) * 1000n + BigInt(Math.floor(Math.random() * 1000))
  const salt = BigInt(
    keccak256(
      encodeAbiParameters(
        [{ type: 'bytes32' }, { type: 'address' }, { type: 'uint256' }],
        [keccak256(stringToHex('OwnedResolver')), callerAddress, version]
      )
    )
  )
  const initData = encodeFunctionData({
    abi: resolverInitAbi,
    functionName: 'initialize',
    args: [[{ account: callerAddress, roleBitmap: ALL_ROLES }], []],
  })
  const deployReceipt = await writeAndWait('deploy resolver proxy', {
    address: VERIFIABLE_FACTORY,
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [PERMISSIONED_RESOLVER_IMPL, salt, initData],
  })
  const [deployLog] = parseEventLogs({
    abi: verifiableFactoryAbi,
    eventName: 'ProxyDeployed',
    logs: deployReceipt.logs,
  })
  const resolverAddress = deployLog.args.proxyAddress
  console.log(`   resolver: ${resolverAddress}`)

  console.log(`\n[2/3] Registering ${deviceLabel}.${tailnetName()} (requires ROLE_REGISTRAR)...`)
  const farExpiry = BigInt(Math.floor(Date.now() / 1000) + expiryYears * 365 * 24 * 60 * 60)
  const zeroAddress = '0x0000000000000000000000000000000000000000' as const
  await writeAndWait('register', {
    address: tailnetRegistry(),
    abi: registryWriteAbi,
    functionName: 'register',
    args: [deviceLabel, callerAddress, zeroAddress, resolverAddress, REGISTRATION_ROLE_BITMAP, farExpiry],
  })

  console.log('\n[3/3] Writing initial pubkey...')
  const fullname = normalize(`${deviceLabel}.${tailnetName()}`)
  const dnsName = toHex(packetToBytes(fullname))
  await writeAndWait('setText(pubkey)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'setText',
    args: [dnsName, 'pubkey', pubkey],
  })

  const readBack = await publicClient.getEnsText({ name: fullname, key: 'pubkey' })
  if (readBack !== pubkey) throw new Error(`pubkey did not round-trip: got ${JSON.stringify(readBack)}`)

  console.log(`\ndone. ${fullname} enrolled, resolver ${resolverAddress}, pubkey ${readBack}.`)
}

main().catch((err) => {
  console.error('\nenroll failed:', err)
  process.exit(1)
})
