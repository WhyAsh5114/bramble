// Registers a relay subname on the relay registry (docs/adr/0003's
// Consequence section, resolved) — mirrors enroll.ts's "deploy a dedicated
// resolver, register, write initial records" exactly, just targeting the
// relay registry instead of the tailnet's device registry, and writing
// endpoint-shaped records deliberately: relay subnames are the one
// namespace Gate 1.4 allows to carry them (docs/05_BUILD_PLAN.md Gate 1.4),
// because they live in a structurally separate registry contract
// (setup-relay-registry.ts) device/agent write paths never touch.
//
// Requires ROLE_REGISTRAR on the relay registry — granted via
// `grant-role.ts relay-registrar <address>`.
//
// A relay can be either role, or both — pass whichever flags apply:
// Usage: bun run register-relay.ts <relay-label> [--rendezvous host:port] [--sidecar-url url] [--price-per-byte n]
import {
  createWalletClient,
  http,
  encodeAbiParameters,
  encodeFunctionData,
  parseEventLogs,
  parseAbi,
  keccak256,
  stringToHex,
  toHex,
} from 'viem'
import { packetToBytes } from 'viem/ens'
import {
  RPC_URL,
  tailnetName,
  relayRegistry,
  VERIFIABLE_FACTORY,
  PERMISSIONED_RESOLVER_IMPL,
  ALL_ROLES,
  REGISTRATION_ROLE_BITMAP,
} from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { registryWriteAbi, resolverWriteAbi } from './roles'

const verifiableFactoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
  'event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)',
])
const resolverInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants, bytes[] calls)'])

function parseFlags(args: string[]): { rendezvous?: string; sidecarUrl?: string; pricePerByte?: string } {
  const out: { rendezvous?: string; sidecarUrl?: string; pricePerByte?: string } = {}
  for (let i = 0; i < args.length; i += 2) {
    const [flag, value] = [args[i], args[i + 1]]
    if (flag === '--rendezvous') out.rendezvous = value
    else if (flag === '--sidecar-url') out.sidecarUrl = value
    else if (flag === '--price-per-byte') out.pricePerByte = value
    else throw new Error(`unknown flag ${flag}`)
  }
  return out
}

async function main() {
  const [relayLabel, ...rest] = process.argv.slice(2)
  if (!relayLabel) {
    throw new Error(
      'usage: bun run register-relay.ts <relay-label> [--rendezvous host:port] [--sidecar-url url] [--price-per-byte n]'
    )
  }
  const flags = parseFlags(rest)
  if (!flags.rendezvous && !flags.sidecarUrl) {
    throw new Error(
      'at least one of --rendezvous or --sidecar-url is required — a relay with neither offers nothing discoverable'
    )
  }

  const registry = relayRegistry()
  if (!registry) throw new Error('BRAMBLE_RELAY_REGISTRY not set — run setup-relay-registry.ts first')

  const account = accountFromEnv()
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  async function writeAndWait(label: string, args: Parameters<typeof wallet.writeContract>[0]) {
    const hash = await wallet.writeContract(args)
    console.log(`   tx (${label}): ${hash}`)
    const receipt = await publicClient.waitForTransactionReceipt({ hash })
    if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
    return receipt
  }

  console.log(`[1/3] Deploying a dedicated resolver for ${relayLabel}...`)
  const version = BigInt(Date.now()) * 1000n + BigInt(Math.floor(Math.random() * 1000))
  const salt = BigInt(
    keccak256(
      encodeAbiParameters(
        [{ type: 'bytes32' }, { type: 'address' }, { type: 'uint256' }],
        [keccak256(stringToHex('OwnedResolver')), account.address, version]
      )
    )
  )
  const initData = encodeFunctionData({
    abi: resolverInitAbi,
    functionName: 'initialize',
    args: [[{ account: account.address, roleBitmap: ALL_ROLES }], []],
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

  const fullname = `${relayLabel}.relays.${tailnetName()}`
  console.log(`\n[2/3] Registering ${fullname} on the relay registry (requires ROLE_REGISTRAR)...`)
  const farExpiry = BigInt(Math.floor(Date.now() / 1000) + 10 * 365 * 24 * 60 * 60)
  const zeroAddress = '0x0000000000000000000000000000000000000000' as const
  await writeAndWait('register', {
    address: registry,
    abi: registryWriteAbi,
    functionName: 'register',
    args: [relayLabel, account.address, zeroAddress, resolverAddress, REGISTRATION_ROLE_BITMAP, farExpiry],
  })

  console.log('\n[3/3] Writing relay records...')
  const dnsName = toHex(packetToBytes(fullname))
  if (flags.rendezvous) {
    await writeAndWait('setText(rendezvous-address)', {
      address: resolverAddress,
      abi: resolverWriteAbi,
      functionName: 'setText',
      args: [dnsName, 'rendezvous-address', flags.rendezvous],
    })
  }
  if (flags.sidecarUrl) {
    await writeAndWait('setText(sidecar-url)', {
      address: resolverAddress,
      abi: resolverWriteAbi,
      functionName: 'setText',
      args: [dnsName, 'sidecar-url', flags.sidecarUrl],
    })
    await writeAndWait('setText(price-per-byte)', {
      address: resolverAddress,
      abi: resolverWriteAbi,
      functionName: 'setText',
      args: [dnsName, 'price-per-byte', flags.pricePerByte ?? '1'],
    })
  }

  console.log(`\ndone. ${fullname} registered, resolver ${resolverAddress}.`)
}

main().catch((err) => {
  console.error('\nregister-relay failed:', err)
  process.exit(1)
})
