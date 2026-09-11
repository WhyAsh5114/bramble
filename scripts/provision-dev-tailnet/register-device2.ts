// One-off: registers a second device subname ("device2") on the already
// deployed dev tailnet subregistry, for Gate 0.2's real two-machine run
// (needs two independently ENS-resolvable devices, one per machine). No
// commit-reveal here — that's only required for the top-level .eth name
// registration (already done in index.ts); a device subname is registered
// directly on the tailnet's own UserRegistry subregistry.
//
// Usage: bun run register-device2.ts <vps-pubkey-hex>
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  createPublicClient,
  createWalletClient,
  http,
  parseAbi,
  getAddress,
  encodeFunctionData,
  keccak256,
  stringToHex,
  encodeAbiParameters,
  toHex,
  parseEventLogs,
} from 'viem'
import { sepolia } from 'viem/chains'
import { privateKeyToAccount } from 'viem/accounts'
import { normalize, packetToBytes } from 'viem/ens'
import {
  VERIFIABLE_FACTORY,
  PERMISSIONED_RESOLVER_IMPL,
  UNIVERSAL_RESOLVER,
  ALL_ROLES,
  RPC_URL,
} from '../../sidecar/src/ens/config'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const envPath = path.resolve(__dirname, '../../.env')
if (existsSync(envPath)) {
  const env = await Bun.file(envPath).text()
  for (const line of env.split('\n')) {
    const match = line.match(/^([^#=]+)=(.*)$/)
    if (match && !process.env[match[1].trim()]) {
      // Strip one layer of matching surrounding quotes — see
      // admincli/src/setup.ts's identical loader for the full reasoning.
      const rawValue = match[2].trim()
      process.env[match[1].trim()] = rawValue.replace(/^(['"])(.*)\1$/, '$2')
    }
  }
}

const [vpsPubkey] = process.argv.slice(2)
if (!vpsPubkey) throw new Error('usage: bun run register-device2.ts <vps-pubkey-hex>')

if (!process.env.SEPOLIA_PRIVATE_KEY) throw new Error('SEPOLIA_PRIVATE_KEY not found in .env')
const rawKey = process.env.SEPOLIA_PRIVATE_KEY.trim()
const privateKey = (rawKey.startsWith('0x') ? rawKey : `0x${rawKey}`) as `0x${string}`
const account = privateKeyToAccount(privateKey)

const hackathonSepolia = {
  ...sepolia,
  contracts: { ...sepolia.contracts, ensUniversalResolver: { address: getAddress(UNIVERSAL_RESOLVER) } },
}
const publicClient = createPublicClient({ chain: hackathonSepolia, transport: http(RPC_URL) })
const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

async function writeAndWait(label: string, args: Parameters<typeof wallet.writeContract>[0]) {
  const hash = await wallet.writeContract(args)
  console.log(`   tx (${label}): ${hash}`)
  const receipt = await publicClient.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
  return receipt
}

const verifiableFactoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
  'event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)',
])
const resolverInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants, bytes[] calls)'])
const registryAbi = parseAbi([
  'function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256 tokenId)',
])
const resolverAbi = parseAbi(['function setText(bytes name, string key, string value)'])

const fixturePath = path.resolve(__dirname, '../../sidecar/test/fixtures/dev-tailnet.json')
const fixture = JSON.parse(readFileSync(fixturePath, 'utf-8'))
const subRegistryAddress = getAddress(fixture.tailnetRegistry)
const tailnetName = fixture.tailnetName as string

console.log(`Registering device2 on subregistry ${subRegistryAddress} (${tailnetName})...`)

console.log('\n[1/3] Deploying a dedicated resolver for device2...')
const salt = BigInt(
  keccak256(
    encodeAbiParameters(
      [{ type: 'bytes32' }, { type: 'address' }, { type: 'uint256' }],
      [keccak256(stringToHex('OwnedResolver-device2')), account.address, BigInt(Date.now())]
    )
  )
)
const initData = encodeFunctionData({
  abi: resolverInitAbi,
  functionName: 'initialize',
  args: [[{ account: account.address, roleBitmap: ALL_ROLES }], []],
})
const deployReceipt = await writeAndWait('deploy device2 resolver proxy', {
  address: getAddress(VERIFIABLE_FACTORY),
  abi: verifiableFactoryAbi,
  functionName: 'deployProxy',
  args: [getAddress(PERMISSIONED_RESOLVER_IMPL), salt, initData],
})
const [deployedLog] = parseEventLogs({
  abi: verifiableFactoryAbi,
  eventName: 'ProxyDeployed',
  logs: deployReceipt.logs,
})
const deviceResolverAddress = deployedLog.args.proxyAddress
console.log(`   device2 resolver: ${deviceResolverAddress}`)

console.log('\n[2/3] Registering device2 subname on the tailnet subregistry...')
const deviceLabel = 'device2'
const zeroAddress = '0x0000000000000000000000000000000000000000' as const
const farExpiry = BigInt(Math.floor(Date.now() / 1000) + 10 * 365 * 24 * 60 * 60)
await writeAndWait('register device2 subname', {
  address: subRegistryAddress,
  abi: registryAbi,
  functionName: 'register',
  args: [deviceLabel, account.address, zeroAddress, deviceResolverAddress, ALL_ROLES, farExpiry],
})

console.log('\n[3/3] Writing pubkey text record on device2...')
const deviceFullName = `${deviceLabel}.${tailnetName}`
const normalizedDeviceName = normalize(deviceFullName)
const dnsNameDevice = toHex(packetToBytes(normalizedDeviceName))
await writeAndWait('setText(pubkey)', {
  address: deviceResolverAddress,
  abi: resolverAbi,
  functionName: 'setText',
  args: [dnsNameDevice, 'pubkey', vpsPubkey],
})

const readBack = await publicClient.getEnsText({ name: normalizedDeviceName, key: 'pubkey' })
if (readBack !== vpsPubkey) throw new Error(`pubkey did not round-trip: got "${readBack}"`)

fixture.device2Label = deviceLabel
fixture.device2Fullname = normalizedDeviceName
fixture.device2ExpectedPubkey = vpsPubkey
writeFileSync(fixturePath, JSON.stringify(fixture, null, 2) + '\n')

console.log('\n================ DEVICE2 REGISTERED ================')
console.log(`fullname: ${normalizedDeviceName}`)
console.log(`pubkey:   ${vpsPubkey}`)
console.log(`Fixture updated: ${fixturePath}`)
