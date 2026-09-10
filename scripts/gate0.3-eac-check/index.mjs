// Gate 0.3 / K2 verification script.
//
// Registers a real .eth name on the ETHOnline 2026 hackathon ENSv2 Sepolia
// deployment, gives it its own Permissioned Resolver, creates a subname
// (mirroring bramble's device-subname pattern) with ITS OWN resolver,
// writes/reads a text record, then delegates exactly one scoped EAC role
// (ROLE_SET_TEXT on a single text key) to an address that holds no other
// grants, and confirms on-chain that:
//   (a) that address CAN write the one key it was granted, and
//   (b) that address CANNOT write any other key or record type.
//
// (b) is the actual K2 kill-criterion check from 07_RISKS.md: if a
// delegated account can act outside its grant, EAC doesn't restrict and
// the ENS pitch collapses. This script proves it does or doesn't, against
// the real deployed hackathon contracts, not the general ENSv2 beta.
//
// Usage: node index.mjs   (run from this directory; reads ../../.env)

import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { config } from 'dotenv'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
config({ path: path.resolve(__dirname, '../../.env') })

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
import { privateKeyToAccount, generatePrivateKey } from 'viem/accounts'
import { normalize, packetToBytes } from 'viem/ens'

// ---------------------------------------------------------------------------
// Hackathon ENSv2 Sepolia deployment addresses.
// Source: https://feature-permres-inode-refact.docs-bao.pages.dev/learn/deployments#sepolia-ensv2-beta
// Cross-checked against Kevin | ENS's Discord post, Sept 4 2026.
// ---------------------------------------------------------------------------
const ETH_REGISTRY = getAddress('0x1d78834d97c1d7b1a38c1dedbd1a287cfed3971e') // Permissioned Registry for .eth
const ETH_REGISTRAR = getAddress('0x7d1b7f586a62ac3f54b9a396849757814283270b')
const MOCK_USDC = getAddress('0xcbfd80f74375c54e545af34788ff465f96f66f05')
const VERIFIABLE_FACTORY = getAddress('0x894bc9cc8ff1ad96b8a288c86a8c71d662c07780')
const PERMISSIONED_RESOLVER_IMPL = getAddress('0xa9d3814ab151bf6e37a427432795371a8361614e')
const USER_REGISTRY_IMPL = getAddress('0x47b442d0cf617c41cabaff5f02f44dd1e5f72546')
const UNIVERSAL_RESOLVER = getAddress('0xd26f2040d083af1cd2962ba303f4bea0c4faf142') // UpgradableUniversalResolverProxy

// ---------------------------------------------------------------------------
// EAC role constants (values from the ENSv2 docs' Permissioned Registry /
// Permissioned Resolver role tables, not guessed).
// ---------------------------------------------------------------------------
const ROLE_SET_SUBREGISTRY = 1n << 20n
const ROLE_SET_RESOLVER = 1n << 24n
const ROLE_CAN_TRANSFER_ADMIN = (1n << 28n) << 128n
const REGISTRATION_ROLE_BITMAP =
  ROLE_SET_SUBREGISTRY |
  (ROLE_SET_SUBREGISTRY << 128n) | // ROLE_SET_SUBREGISTRY_ADMIN
  ROLE_SET_RESOLVER |
  (ROLE_SET_RESOLVER << 128n) | // ROLE_SET_RESOLVER_ADMIN
  ROLE_CAN_TRANSFER_ADMIN

const ROLE_SET_TEXT = 1n << 4n
const ROLE_SET_ADDRESS = 1n << 0n

// Every regular + admin role bit set (bit 0 of all 64 nybbles), computed
// rather than copy-pasted, to avoid a uint256-overflow typo.
const ALL_ROLES = BigInt('0x' + '1'.repeat(64))

// ---------------------------------------------------------------------------
// ABIs — minimal, hand-written from the docs' function/event tables.
// ---------------------------------------------------------------------------
const erc20Abi = parseAbi([
  'function mint(address to, uint256 amount)',
  'function approve(address spender, uint256 amount) returns (bool)',
  'function balanceOf(address account) view returns (uint256)',
])

const ethRegistrarAbi = parseAbi([
  'function isAvailable(string label) view returns (bool)',
  'function getRegisterPrice(string label, uint64 duration, address paymentToken) view returns (uint256 base, uint256 premium)',
  'function makeCommitment(string label, address owner, bytes32 secret, address subregistry, address resolver, uint64 duration, bytes32 referrer) view returns (bytes32)',
  'function commit(bytes32 commitment)',
  'function register(string label, address owner, bytes32 secret, address subregistry, address resolver, uint64 duration, address paymentToken, bytes32 referrer) returns (uint256 tokenId)',
])

const registryAbi = parseAbi([
  'function ownerOf(uint256 id) view returns (address)',
  'function getState(uint256 anyId) view returns (uint8 status, uint64 expiry, address latestOwner, uint256 tokenId, uint256 resource)',
  'function setSubregistry(uint256 anyId, address registry)',
  'function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256 tokenId)',
])

const verifiableFactoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
  'event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)',
])

const resolverInitAbi = parseAbi([
  'function initialize((address account, uint256 roleBitmap)[] grants, bytes[] calls)',
])

// NOTE: the ENSv2 docs page for this step shows `initialize(address rootAccount,
// uint256 roleBitmap)`, but the actually-deployed UserRegistryImpl on this
// hackathon deployment (verified source on Sepolia Etherscan) uses a single
// Grant[] array instead, same shape as the resolver's grants but without the
// `calls` array. Confirmed empirically: the two-arg version reverts inside
// the nested initializer call every time; this one succeeds. Docs vs. deployed
// bytecode drift — noted in docs/11_SOURCE_NOTES.md.
const userRegistryInitAbi = parseAbi([
  'function initialize((address account, uint256 roleBitmap)[] grants)',
])

const resolverAbi = parseAbi([
  'function setText(bytes name, string key, string value)',
  'function setAddress(bytes name, uint256 coinType, bytes value)',
  'function grantSetterRoles(bytes setter, address account) returns (bool)',
])

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------
if (!process.env.SEPOLIA_PRIVATE_KEY) {
  throw new Error('SEPOLIA_PRIVATE_KEY not found in .env')
}
const rawKey = process.env.SEPOLIA_PRIVATE_KEY.trim()
const privateKey = rawKey.startsWith('0x') ? rawKey : `0x${rawKey}`
const account = privateKeyToAccount(privateKey)
const rpcUrl = process.env.SEPOLIA_RPC_URL || 'https://ethereum-sepolia-rpc.publicnode.com'

const hackathonSepolia = {
  ...sepolia,
  contracts: {
    ...sepolia.contracts,
    ensUniversalResolver: { address: UNIVERSAL_RESOLVER },
  },
}

const publicClient = createPublicClient({ chain: hackathonSepolia, transport: http(rpcUrl) })
const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(rpcUrl) })

const results = {}
function report(name, pass, detail) {
  results[name] = pass
  console.log(`${pass ? '✅ PASS' : '❌ FAIL'} — ${name}${detail ? ': ' + detail : ''}`)
}

async function writeAndWait(label, args) {
  const hash = await wallet.writeContract(args)
  console.log(`   tx (${label}): ${hash}`)
  const receipt = await publicClient.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
  return receipt
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
async function main() {
  console.log(`Account: ${account.address}`)
  const ethBalance = await publicClient.getBalance({ address: account.address })
  console.log(`Sepolia ETH balance: ${formatEther(ethBalance)}`)
  if (ethBalance === 0n) {
    throw new Error(
      'Account has 0 Sepolia ETH. Fund it via a public Sepolia faucet before running this script (needed for gas).'
    )
  }

  // 1. Mint test funds (MockUSDC has no access control on mint).
  console.log('\n[1/8] Minting MockUSDC...')
  await writeAndWait('mint MockUSDC', {
    address: MOCK_USDC,
    abi: erc20Abi,
    functionName: 'mint',
    args: [account.address, 1_000_000_000n], // 1000 USDC, 6 decimals
  })

  // 2. Deploy a Permissioned Resolver for the top-level test name.
  // Resolver proxy addresses are deterministic (CREATE2 keyed on owner+version),
  // so a fixed version collides with any previous run's deployment. Randomize
  // per run instead of hardcoding 0n/1n.
  const runSeed = BigInt(Date.now()) * 1000n + BigInt(Math.floor(Math.random() * 1000))
  console.log('\n[2/8] Deploying root Permissioned Resolver proxy...')
  const rootResolverAddress = await deployResolverProxy(runSeed)
  console.log(`   root resolver: ${rootResolverAddress}`)

  // 3. Register a test name via the ETH Registrar (commit-reveal).
  const label = `bramble-g03-${Math.random().toString(16).slice(2, 10)}`
  const fullName = `${label}.eth`
  console.log(`\n[3/8] Registering ${fullName}...`)

  const available = await publicClient.readContract({
    address: ETH_REGISTRAR,
    abi: ethRegistrarAbi,
    functionName: 'isAvailable',
    args: [label],
  })
  report('name is available before registering', available)
  if (!available) throw new Error('chosen label unexpectedly taken, rerun the script')

  const duration = 31536000n // 1 year
  const secret = keccak256(toHex(crypto.randomUUID()))
  const referrer = `0x${'0'.repeat(64)}`
  const zeroAddress = '0x0000000000000000000000000000000000000000'

  const [base, premium] = await publicClient.readContract({
    address: ETH_REGISTRAR,
    abi: ethRegistrarAbi,
    functionName: 'getRegisterPrice',
    args: [label, duration, MOCK_USDC],
  })
  const totalCost = base + premium
  console.log(`   price: ${totalCost} (base ${base} + premium ${premium})`)

  await writeAndWait('approve MockUSDC for registrar', {
    address: MOCK_USDC,
    abi: erc20Abi,
    functionName: 'approve',
    args: [ETH_REGISTRAR, totalCost],
  })

  const commitment = await publicClient.readContract({
    address: ETH_REGISTRAR,
    abi: ethRegistrarAbi,
    functionName: 'makeCommitment',
    args: [label, account.address, secret, zeroAddress, rootResolverAddress, duration, referrer],
  })
  await writeAndWait('commit', {
    address: ETH_REGISTRAR,
    abi: ethRegistrarAbi,
    functionName: 'commit',
    args: [commitment],
  })

  console.log('   waiting 75s for MIN_COMMITMENT_AGE (60s)...')
  await sleep(75_000)

  const registerReceipt = await writeAndWait('register', {
    address: ETH_REGISTRAR,
    abi: ethRegistrarAbi,
    functionName: 'register',
    args: [label, account.address, secret, zeroAddress, rootResolverAddress, duration, MOCK_USDC, referrer],
  })

  // NOTE: ownerOf() requires the exact current (version-matched) token ID and
  // returns address(0) for a "stale" ID — passing the raw labelhash directly
  // fails there even though the docs describe broad anyId polymorphism.
  // getState() is the function actually documented to accept a bare labelhash.
  const labelhash = BigInt(keccak256(toHex(label)))
  const state = await publicClient.readContract({
    address: ETH_REGISTRY,
    abi: registryAbi,
    functionName: 'getState',
    args: [labelhash],
  })
  const [, , latestOwner, tokenId] = state
  report('registration succeeded, caller is owner', latestOwner.toLowerCase() === account.address.toLowerCase(), `tokenId=${tokenId}`)

  // 4. Write + read a text record on the parent name.
  console.log('\n[4/8] Writing + reading a text record on the parent name...')
  const normalizedFullName = normalize(fullName)
  const dnsNameFull = toHex(packetToBytes(normalizedFullName))
  await writeAndWait('setText(description) on parent', {
    address: rootResolverAddress,
    abi: resolverAbi,
    functionName: 'setText',
    args: [dnsNameFull, 'description', 'bramble gate 0.3 test'],
  })
  const readBack = await publicClient.getEnsText({ name: normalizedFullName, key: 'description' })
  report('text record round-trips through Universal Resolver', readBack === 'bramble gate 0.3 test', `got "${readBack}"`)

  // 5. Deploy a UserRegistry subregistry for the test name, and point the
  //    name at it — mirrors bramble's "each tailnet has its own subregistry
  //    of devices" pattern.
  console.log('\n[5/8] Deploying subregistry and attaching it to the test name...')
  const subRegistryAddress = await deployUserRegistryProxy(fullName, 0n)
  console.log(`   subregistry: ${subRegistryAddress}`)
  await writeAndWait('setSubregistry', {
    address: ETH_REGISTRY,
    abi: registryAbi,
    functionName: 'setSubregistry',
    args: [tokenId, subRegistryAddress], // use the exact current tokenId, not a re-derived labelhash
  })

  // 6. Deploy a SEPARATE Permissioned Resolver for the subname — "a subname
  //    with its own Permissioned Resolver," per Gate 0.3, distinct from the
  //    parent's.
  console.log('\n[6/8] Deploying a dedicated resolver for the subname...')
  const deviceResolverAddress = await deployResolverProxy(runSeed + 1n) // different version -> different deterministic address
  console.log(`   device resolver: ${deviceResolverAddress}`)

  const deviceLabel = 'device1'
  const deviceFullName = `${deviceLabel}.${fullName}`
  const farExpiry = BigInt(Math.floor(Date.now() / 1000) + 10 * 365 * 24 * 60 * 60)
  await writeAndWait('register subname on subregistry', {
    address: subRegistryAddress,
    abi: registryAbi,
    functionName: 'register',
    args: [deviceLabel, account.address, zeroAddress, deviceResolverAddress, ALL_ROLES, farExpiry],
  })

  const normalizedDeviceName = normalize(deviceFullName)
  const dnsNameDevice = toHex(packetToBytes(normalizedDeviceName))
  await writeAndWait('setText(pubkey) on subname', {
    address: deviceResolverAddress,
    abi: resolverAbi,
    functionName: 'setText',
    args: [dnsNameDevice, 'pubkey', 'test-wireguard-pubkey-value'],
  })
  const devicePubkey = await publicClient.getEnsText({ name: normalizedDeviceName, key: 'pubkey' })
  report(
    'subname text record round-trips through full hierarchy',
    devicePubkey === 'test-wireguard-pubkey-value',
    `${deviceFullName} -> pubkey = "${devicePubkey}"`
  )

  // 7. THE ACTUAL K2 / GATE 0.3 TEST: delegate ONE scoped role, confirm it
  //    both works AND doesn't leak beyond its grant.
  console.log('\n[7/8] Delegating a single scoped EAC role (ROLE_SET_TEXT on key "status")...')
  const unauthorized = privateKeyToAccount(generatePrivateKey()).address
  console.log(`   unauthorized test address (never funded, never signs): ${unauthorized}`)

  const statusSetter = encodeFunctionData({
    abi: resolverAbi,
    functionName: 'setText',
    args: ['0x', 'status', ''],
  })
  await writeAndWait('grantSetterRoles(status -> unauthorized)', {
    address: deviceResolverAddress,
    abi: resolverAbi,
    functionName: 'grantSetterRoles',
    args: [statusSetter, unauthorized],
  })

  // 8. Confirm the delegated action succeeds, and everything else reverts.
  console.log('\n[8/8] Verifying the delegation is scoped exactly as granted...')

  let grantedActionWorked = false
  try {
    await publicClient.simulateContract({
      account: unauthorized,
      address: deviceResolverAddress,
      abi: resolverAbi,
      functionName: 'setText',
      args: [dnsNameDevice, 'status', 'revoked'],
    })
    grantedActionWorked = true
  } catch (e) {
    grantedActionWorked = false
  }
  report('delegated account CAN write the granted key ("status")', grantedActionWorked)

  let pubkeyBlocked = false
  try {
    await publicClient.simulateContract({
      account: unauthorized,
      address: deviceResolverAddress,
      abi: resolverAbi,
      functionName: 'setText',
      args: [dnsNameDevice, 'pubkey', 'malicious-pubkey-overwrite'],
    })
    pubkeyBlocked = false
  } catch (e) {
    pubkeyBlocked = /EACUnauthorizedAccountRoles|reverted/i.test(String(e.shortMessage || e.message))
    if (!pubkeyBlocked) console.log('   (unexpected revert reason, see raw error below)')
    if (!pubkeyBlocked) console.log('  ', e.shortMessage || e.message)
  }
  report('delegated account CANNOT write a different key ("pubkey") — this is K2', pubkeyBlocked)

  let addressBlocked = false
  try {
    await publicClient.simulateContract({
      account: unauthorized,
      address: deviceResolverAddress,
      abi: resolverAbi,
      functionName: 'setAddress',
      args: [dnsNameDevice, 60n, unauthorized],
    })
    addressBlocked = false
  } catch (e) {
    addressBlocked = /EACUnauthorizedAccountRoles|reverted/i.test(String(e.shortMessage || e.message))
  }
  report('delegated account CANNOT set an address record (different role entirely)', addressBlocked)

  // ---------------------------------------------------------------------
  console.log('\n================ GATE 0.3 / K2 SUMMARY ================')
  console.log(`Test name:      ${fullName}`)
  console.log(`Device subname: ${deviceFullName}`)
  console.log(`Root resolver:  ${rootResolverAddress}`)
  console.log(`Device resolver:${deviceResolverAddress}`)
  console.log(`Subregistry:    ${subRegistryAddress}`)
  const allPassed = Object.values(results).every(Boolean)
  console.log(allPassed ? '\n✅ ALL CHECKS PASSED — EAC restricts as documented on the hackathon deployment.' : '\n❌ AT LEAST ONE CHECK FAILED — see above, this is a K2 finding.')
  process.exit(allPassed ? 0 : 1)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
async function deployResolverProxy(version) {
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
    address: VERIFIABLE_FACTORY,
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [PERMISSIONED_RESOLVER_IMPL, salt, initData],
  })
  const [log] = parseEventLogs({ abi: verifiableFactoryAbi, eventName: 'ProxyDeployed', logs: receipt.logs })
  return log.args.proxyAddress
}

async function deployUserRegistryProxy(fullName, version) {
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
    address: VERIFIABLE_FACTORY,
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [USER_REGISTRY_IMPL, salt, initData],
  })
  const [log] = parseEventLogs({ abi: verifiableFactoryAbi, eventName: 'ProxyDeployed', logs: receipt.logs })
  return log.args.proxyAddress
}

main().catch((err) => {
  console.error('\n❌ SCRIPT ERROR:', err)
  process.exit(1)
})
