// One-time setup script for Phase 1 Section A: registers a persistent test
// tailnet (a name + subregistry + one device subname) on the hackathon
// ENSv2 Sepolia deployment, so the sidecar has real on-chain data to resolve
// without re-registering (commit-reveal + 75s wait) on every test run.
//
// Adapted from scripts/gate0.3-eac-check/index.mjs, steps 1-6 (register,
// text record, subregistry, device subname) — the EAC-delegation test part
// (steps 7-8) was Gate 0.3-specific and isn't needed here.
//
// Usage: bun run index.ts   (run from this directory; reads ../../.env)
import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  createPublicClient,
  createWalletClient,
  http,
  parseAbi,
  encodeAbiParameters,
  encodeFunctionData,
  parseEventLogs,
  keccak256,
  toHex,
  stringToHex,
  namehash,
  getAddress,
  formatEther,
} from 'viem'
import { sepolia } from 'viem/chains'
import { privateKeyToAccount } from 'viem/accounts'
import { normalize, packetToBytes } from 'viem/ens'
import {
  ETH_REGISTRY,
  ETH_REGISTRAR,
  MOCK_USDC,
  VERIFIABLE_FACTORY,
  PERMISSIONED_RESOLVER_IMPL,
  USER_REGISTRY_IMPL,
  UNIVERSAL_RESOLVER,
  ALL_ROLES,
  RPC_URL,
} from '../../sidecar/src/ens/config'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// Bun loads .env natively — no dotenv dependency needed.
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

const ethRegistrarAbi = parseAbi([
  'function isAvailable(string label) view returns (bool)',
  'function getRegisterPrice(string label, uint64 duration, address paymentToken) view returns (uint256 base, uint256 premium)',
  'function makeCommitment(string label, address owner, bytes32 secret, address subregistry, address resolver, uint64 duration, bytes32 referrer) view returns (bytes32)',
  'function commit(bytes32 commitment)',
  'function register(string label, address owner, bytes32 secret, address subregistry, address resolver, uint64 duration, address paymentToken, bytes32 referrer) returns (uint256 tokenId)',
])

const erc20Abi = parseAbi([
  'function mint(address to, uint256 amount)',
  'function approve(address spender, uint256 amount) returns (bool)',
])

const registryAbi = parseAbi([
  'function getState(uint256 anyId) view returns (uint8 status, uint64 expiry, address latestOwner, uint256 tokenId, uint256 resource)',
  'function setSubregistry(uint256 anyId, address registry)',
  'function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256 tokenId)',
])

const verifiableFactoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
  'event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)',
])

const resolverInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants, bytes[] calls)'])

// See docs/11_SOURCE_NOTES.md: the docs' two-arg initializer for
// UserRegistryImpl reverts against the actually-deployed bytecode. This is
// the corrected signature, verified by Gate 0.3.
const userRegistryInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants)'])

const resolverAbi = parseAbi(['function setText(bytes name, string key, string value)'])

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

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

async function deployResolverProxy(version: bigint) {
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
  const receipt = await writeAndWait(`deploy resolver proxy v${version}`, {
    address: getAddress(VERIFIABLE_FACTORY),
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [getAddress(PERMISSIONED_RESOLVER_IMPL), salt, initData],
  })
  const [log] = parseEventLogs({ abi: verifiableFactoryAbi, eventName: 'ProxyDeployed', logs: receipt.logs })
  return log.args.proxyAddress
}

async function deployUserRegistryProxy(fullName: string, version: bigint) {
  const salt = BigInt(
    keccak256(
      encodeAbiParameters(
        [{ type: 'bytes32' }, { type: 'bytes32' }, { type: 'uint256' }],
        [keccak256(stringToHex('UserRegistry')), namehash(fullName), version]
      )
    )
  )
  const initData = encodeFunctionData({
    abi: userRegistryInitAbi,
    functionName: 'initialize',
    args: [[{ account: account.address, roleBitmap: ALL_ROLES }]],
  })
  const receipt = await writeAndWait('deploy UserRegistry proxy', {
    address: getAddress(VERIFIABLE_FACTORY),
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [getAddress(USER_REGISTRY_IMPL), salt, initData],
  })
  const [log] = parseEventLogs({ abi: verifiableFactoryAbi, eventName: 'ProxyDeployed', logs: receipt.logs })
  return log.args.proxyAddress
}

async function main() {
  console.log(`Account: ${account.address}`)
  const ethBalance = await publicClient.getBalance({ address: account.address })
  console.log(`Sepolia ETH balance: ${formatEther(ethBalance)}`)
  if (ethBalance === 0n) throw new Error('Account has 0 Sepolia ETH — fund it via a faucet first.')

  console.log('\n[1/6] Minting MockUSDC...')
  await writeAndWait('mint MockUSDC', {
    address: getAddress(MOCK_USDC),
    abi: erc20Abi,
    functionName: 'mint',
    args: [account.address, 1_000_000_000n],
  })

  const runSeed = BigInt(Date.now()) * 1000n + BigInt(Math.floor(Math.random() * 1000))
  console.log('\n[2/6] Deploying root Permissioned Resolver proxy...')
  const rootResolverAddress = await deployResolverProxy(runSeed)
  console.log(`   root resolver: ${rootResolverAddress}`)

  const label = `bramble-dev-${Math.random().toString(16).slice(2, 10)}`
  const fullName = `${label}.eth`
  console.log(`\n[3/6] Registering ${fullName}...`)

  const duration = 31536000n
  const secret = keccak256(toHex(crypto.randomUUID()))
  const referrer = `0x${'0'.repeat(64)}` as `0x${string}`
  const zeroAddress = '0x0000000000000000000000000000000000000000' as const

  const [base, premium] = await publicClient.readContract({
    address: getAddress(ETH_REGISTRAR),
    abi: ethRegistrarAbi,
    functionName: 'getRegisterPrice',
    args: [label, duration, getAddress(MOCK_USDC)],
  })
  const totalCost = base + premium

  await writeAndWait('approve MockUSDC for registrar', {
    address: getAddress(MOCK_USDC),
    abi: erc20Abi,
    functionName: 'approve',
    args: [getAddress(ETH_REGISTRAR), totalCost],
  })

  const commitment = await publicClient.readContract({
    address: getAddress(ETH_REGISTRAR),
    abi: ethRegistrarAbi,
    functionName: 'makeCommitment',
    args: [label, account.address, secret, zeroAddress, rootResolverAddress, duration, referrer],
  })
  await writeAndWait('commit', {
    address: getAddress(ETH_REGISTRAR),
    abi: ethRegistrarAbi,
    functionName: 'commit',
    args: [commitment],
  })

  console.log('   waiting 75s for MIN_COMMITMENT_AGE (60s)...')
  await sleep(75_000)

  await writeAndWait('register', {
    address: getAddress(ETH_REGISTRAR),
    abi: ethRegistrarAbi,
    functionName: 'register',
    args: [label, account.address, secret, zeroAddress, rootResolverAddress, duration, getAddress(MOCK_USDC), referrer],
  })

  const labelhash = BigInt(keccak256(toHex(label)))
  const state = await publicClient.readContract({
    address: getAddress(ETH_REGISTRY),
    abi: registryAbi,
    functionName: 'getState',
    args: [labelhash],
  })
  const [, , , tokenId] = state
  console.log(`   registered, tokenId=${tokenId}`)

  console.log('\n[4/6] Deploying subregistry and attaching it to the test name...')
  const subRegistryAddress = await deployUserRegistryProxy(fullName, 0n)
  console.log(`   subregistry: ${subRegistryAddress}`)
  await writeAndWait('setSubregistry', {
    address: getAddress(ETH_REGISTRY),
    abi: registryAbi,
    functionName: 'setSubregistry',
    args: [tokenId, subRegistryAddress],
  })

  console.log('\n[5/6] Deploying a dedicated resolver for the device subname...')
  const deviceResolverAddress = await deployResolverProxy(runSeed + 1n)
  console.log(`   device resolver: ${deviceResolverAddress}`)

  const deviceLabel = 'device1'
  const deviceFullName = `${deviceLabel}.${fullName}`
  const farExpiry = BigInt(Math.floor(Date.now() / 1000) + 10 * 365 * 24 * 60 * 60)
  await writeAndWait('register device subname on subregistry', {
    address: subRegistryAddress,
    abi: registryAbi,
    functionName: 'register',
    args: [deviceLabel, account.address, zeroAddress, deviceResolverAddress, ALL_ROLES, farExpiry],
  })

  console.log('\n[6/6] Writing pubkey text record on device subname...')
  const normalizedDeviceName = normalize(deviceFullName)
  const dnsNameDevice = toHex(packetToBytes(normalizedDeviceName))
  const testPubkey = 'bramble-dev-tailnet-test-wireguard-pubkey'
  await writeAndWait('setText(pubkey)', {
    address: deviceResolverAddress,
    abi: resolverAbi,
    functionName: 'setText',
    args: [dnsNameDevice, 'pubkey', testPubkey],
  })
  const readBack = await publicClient.getEnsText({ name: normalizedDeviceName, key: 'pubkey' })
  if (readBack !== testPubkey) throw new Error(`pubkey did not round-trip: got "${readBack}"`)

  const fixture = {
    tailnetName: fullName,
    tailnetRegistry: subRegistryAddress,
    deviceLabel,
    deviceFullname: normalizedDeviceName,
    expectedPubkey: testPubkey,
    provisionedAt: new Date().toISOString(),
  }
  const fixtureDir = path.resolve(__dirname, '../../sidecar/test/fixtures')
  mkdirSync(fixtureDir, { recursive: true })
  const fixturePath = path.join(fixtureDir, 'dev-tailnet.json')
  writeFileSync(fixturePath, JSON.stringify(fixture, null, 2) + '\n')

  console.log('\n================ PROVISIONING COMPLETE ================')
  console.log(JSON.stringify(fixture, null, 2))
  console.log(`\nFixture written to ${fixturePath}`)
}

main().catch((err) => {
  console.error('\nPROVISIONING FAILED:', err)
  process.exit(1)
})
