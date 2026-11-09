// Deploys the shared Bramble name tree on ENSv2 Sepolia:
//
//   bramble.eth            registered through the ETH registrar (MockUSDC, commit-reveal)
//   └─ root registry       UserRegistry owned by the deployer
//      ├─ net.bramble.eth   UserRegistry; OpenRegistrar(net) is its only registrar
//      └─ relay.bramble.eth UserRegistry; OpenRegistrar(relay) is its only registrar
//
// After setup the deployer drops every root role on the two open registries, so nobody
// (including the deployer) can change or remove a claimed tailnet or relay there.
//
// Usage: SEPOLIA_PRIVATE_KEY=... [SEPOLIA_RPC_URL=...] bun contracts/script/deploy-names.ts [--label bramble]
// Progress is saved to contracts/deployments/sepolia.json, so a rerun continues where it stopped.
// The private key is read from the environment and never printed.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  createPublicClient,
  createWalletClient,
  encodeAbiParameters,
  encodeFunctionData,
  getAddress,
  http,
  keccak256,
  parseAbi,
  parseEventLogs,
  stringToHex,
  toHex,
  type Address,
  type Hex,
} from 'viem'
import { privateKeyToAccount } from 'viem/accounts'
import { sepolia } from 'viem/chains'

const here = path.dirname(fileURLToPath(import.meta.url))
const statePath = process.env.DEPLOY_STATE || path.join(here, '..', 'deployments', 'sepolia.json')
const artifactPath = path.join(here, '..', 'out', 'OpenRegistrar.sol', 'OpenRegistrar.json')

const ENS = {
  ethRegistry: getAddress('0xd4ebcbbdf463c9c45784603db0ddd499bc44a8b4'),
  ethRegistrar: getAddress('0xf633e7fc17e2bbe0d0965d18ec1821dcb754a3d3'),
  mockUsdc: getAddress('0x240b0316df57887dbbe58b586508b19e633a14aa'),
  factory: getAddress('0xda70306c98e97ece36f997a21368e53298572991'),
  resolverImpl: getAddress('0x115eb53f0c60696633855f90b138178fb40b2b2c'),
  userRegistryImpl: getAddress('0x9bd8a88719068d09ecee662f36c0e3856708366a'),
}
const ALL_ROLES = BigInt('0x' + '1'.repeat(64))
const ROLE_REGISTRAR = 1n
const ZERO = '0x0000000000000000000000000000000000000000' as const
const TERM_SECONDS = 5n * 365n * 24n * 3600n

const args = process.argv.slice(2)
const labelAt = args.indexOf('--label')
const label = labelAt >= 0 && args[labelAt + 1] ? args[labelAt + 1] : 'bramble'

const rawKey = (process.env.SEPOLIA_PRIVATE_KEY || process.env.SEPOLIA_PK || '').trim()
if (!rawKey) throw new Error('set SEPOLIA_PRIVATE_KEY (or SEPOLIA_PK)')
const account = privateKeyToAccount((rawKey.startsWith('0x') ? rawKey : `0x${rawKey}`) as Hex)
const transport = http(process.env.SEPOLIA_RPC_URL || 'https://ethereum-sepolia-rpc.publicnode.com', {
  timeout: 60_000,
})
const pub = createPublicClient({ chain: sepolia, transport })
const wallet = createWalletClient({ account, chain: sepolia, transport })

const registrarAbi = parseAbi([
  'function isAvailable(string label) view returns (bool)',
  'function MIN_COMMITMENT_AGE() view returns (uint64)',
  'function getRegisterPrice(string label, uint64 duration, address paymentToken) view returns (uint256 base, uint256 premium)',
  'function makeCommitment(string label, address owner, bytes32 secret, address subregistry, address resolver, uint64 duration, bytes32 referrer) view returns (bytes32)',
  'function commit(bytes32 commitment)',
  'function register(string label, address owner, bytes32 secret, address subregistry, address resolver, uint64 duration, address paymentToken, bytes32 referrer) returns (uint256 tokenId)',
])
const erc20Abi = parseAbi([
  'function mint(address to, uint256 amount)',
  'function approve(address spender, uint256 amount) returns (bool)',
  'function balanceOf(address) view returns (uint256)',
])
const registryAbi = parseAbi([
  'function getState(uint256 anyId) view returns (uint8 status, uint64 expiry, address latestOwner, uint256 tokenId, uint256 resource)',
  'function setSubregistry(uint256 anyId, address registry)',
  'function getSubregistry(string label) view returns (address)',
  'function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256 tokenId)',
  'function grantRootRoles(uint256 roleBitmap, address account) returns (bool)',
  'function revokeRootRoles(uint256 roleBitmap, address account) returns (bool)',
  'function hasRootRoles(uint256 roleBitmap, address account) view returns (bool)',
])
const factoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
  'event ProxyDeployed(address indexed sender, address indexed proxyAddress, uint256 salt, address implementation)',
])
const userRegistryInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants)'])
const resolverInitAbi = parseAbi(['function initialize((address account, uint256 roleBitmap)[] grants, bytes[] calls)'])

type State = Record<string, string>
const state: State = existsSync(statePath) ? JSON.parse(readFileSync(statePath, 'utf8')) : {}
function save(key: string, value: string | bigint) {
  state[key] = String(value)
  mkdirSync(path.dirname(statePath), { recursive: true })
  writeFileSync(statePath, JSON.stringify(state, null, 2) + '\n')
  console.log(`  ${key} = ${value}`)
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

// Load-balanced RPCs answer from backends at different heights, so a receipt can be
// "not found" on one request and present on the next. Poll until it shows up.
async function receiptOf(hash: Hex) {
  for (let i = 0; i < 150; i++) {
    try {
      return await pub.getTransactionReceipt({ hash })
    } catch {
      await sleep(4_000)
    }
  }
  throw new Error(`no receipt for ${hash} after 10 minutes`)
}

async function send(what: string, request: Parameters<typeof wallet.writeContract>[0]) {
  const hash = await wallet.writeContract(request)
  console.log(`  ${what}: ${hash}`)
  const receipt = await receiptOf(hash)
  if (receipt.status !== 'success') throw new Error(`${what} reverted`)
  return receipt
}

async function deployProxy(what: string, impl: Address, data: Hex): Promise<{ address: Address; block: bigint }> {
  const salt = BigInt(
    keccak256(encodeAbiParameters([{ type: 'string' }, { type: 'uint256' }], [what, BigInt(Date.now())]))
  )
  const receipt = await send(`deploy ${what}`, {
    address: ENS.factory,
    abi: factoryAbi,
    functionName: 'deployProxy',
    args: [impl, salt, data],
  })
  const [log] = parseEventLogs({ abi: factoryAbi, eventName: 'ProxyDeployed', logs: receipt.logs })
  return { address: log.args.proxyAddress, block: receipt.blockNumber }
}

async function main() {
  console.log(`deployer ${account.address}, name ${label}.eth`)
  const grants = [{ account: account.address, roleBitmap: ALL_ROLES }]

  if (!state.rootResolver) {
    const { address } = await deployProxy(
      `${label}-root-resolver`,
      ENS.resolverImpl,
      encodeFunctionData({ abi: resolverInitAbi, functionName: 'initialize', args: [grants, []] })
    )
    save('rootResolver', address)
  }

  if (!state.ethTokenId) {
    const available = await pub.readContract({
      address: ENS.ethRegistrar,
      abi: registrarAbi,
      functionName: 'isAvailable',
      args: [label],
    })
    if (!available) throw new Error(`${label}.eth is taken`)
    const [base, premium] = await pub.readContract({
      address: ENS.ethRegistrar,
      abi: registrarAbi,
      functionName: 'getRegisterPrice',
      args: [label, TERM_SECONDS, ENS.mockUsdc],
    })
    const cost = base + premium
    const balance = await pub.readContract({
      address: ENS.mockUsdc,
      abi: erc20Abi,
      functionName: 'balanceOf',
      args: [account.address],
    })
    if (balance < cost) {
      await send('mint MockUSDC', {
        address: ENS.mockUsdc,
        abi: erc20Abi,
        functionName: 'mint',
        args: [account.address, cost],
      })
    }
    await send('approve MockUSDC', {
      address: ENS.mockUsdc,
      abi: erc20Abi,
      functionName: 'approve',
      args: [ENS.ethRegistrar, cost],
    })
    const secret = keccak256(toHex(crypto.randomUUID()))
    const referrer = `0x${'0'.repeat(64)}` as Hex
    const resolver = getAddress(state.rootResolver)
    const commitment = await pub.readContract({
      address: ENS.ethRegistrar,
      abi: registrarAbi,
      functionName: 'makeCommitment',
      args: [label, account.address, secret, ZERO, resolver, TERM_SECONDS, referrer],
    })
    await send('commit', { address: ENS.ethRegistrar, abi: registrarAbi, functionName: 'commit', args: [commitment] })
    const age = await pub.readContract({
      address: ENS.ethRegistrar,
      abi: registrarAbi,
      functionName: 'MIN_COMMITMENT_AGE',
    })
    console.log(`  waiting ${age + 15n}s for the commitment`)
    await sleep(Number(age + 15n) * 1000)
    await send('register', {
      address: ENS.ethRegistrar,
      abi: registrarAbi,
      functionName: 'register',
      args: [label, account.address, secret, ZERO, resolver, TERM_SECONDS, ENS.mockUsdc, referrer],
    })
    const [, , , tokenId] = await pub.readContract({
      address: ENS.ethRegistry,
      abi: registryAbi,
      functionName: 'getState',
      args: [BigInt(keccak256(stringToHex(label)))],
    })
    save('ethTokenId', tokenId)
  }

  const userRegistryInit = encodeFunctionData({ abi: userRegistryInitAbi, functionName: 'initialize', args: [grants] })

  if (!state.rootRegistry) {
    const { address } = await deployProxy(`${label}-root-registry`, ENS.userRegistryImpl, userRegistryInit)
    save('rootRegistry', address)
  }
  const rootRegistry = getAddress(state.rootRegistry)
  const attached = await pub.readContract({
    address: ENS.ethRegistry,
    abi: registryAbi,
    functionName: 'getSubregistry',
    args: [label],
  })
  if (getAddress(attached) !== rootRegistry) {
    await send('attach root registry', {
      address: ENS.ethRegistry,
      abi: registryAbi,
      functionName: 'setSubregistry',
      args: [BigInt(state.ethTokenId), rootRegistry],
    })
  }

  const bytecode = JSON.parse(readFileSync(artifactPath, 'utf8')).bytecode.object as Hex
  const openRegistrarAbi = parseAbi([
    'constructor(address registry, address factory, address userRegistryImpl, address resolverImpl)',
  ])

  for (const kind of ['net', 'relay'] as const) {
    if (!state[`${kind}Registry`]) {
      const { address, block } = await deployProxy(`${label}-${kind}-registry`, ENS.userRegistryImpl, userRegistryInit)
      save(`${kind}Registry`, address)
      save(`${kind}RegistryDeployBlock`, block)
    }
    const registry = getAddress(state[`${kind}Registry`])
    const linked = await pub.readContract({
      address: rootRegistry,
      abi: registryAbi,
      functionName: 'getSubregistry',
      args: [kind],
    })
    if (getAddress(linked) !== registry) {
      await send(`register ${kind}.${label}.eth`, {
        address: rootRegistry,
        abi: registryAbi,
        functionName: 'register',
        args: [
          kind,
          account.address,
          registry,
          getAddress(state.rootResolver),
          ALL_ROLES,
          BigInt(Math.floor(Date.now() / 1000)) + TERM_SECONDS,
        ],
      })
    }
    if (!state[`${kind}OpenRegistrar`]) {
      const hash = await wallet.deployContract({
        abi: openRegistrarAbi,
        bytecode,
        args: [registry, ENS.factory, ENS.userRegistryImpl, ENS.resolverImpl],
      })
      console.log(`  deploy OpenRegistrar(${kind}): ${hash}`)
      const receipt = await receiptOf(hash)
      if (receipt.status !== 'success' || !receipt.contractAddress) throw new Error('OpenRegistrar deploy failed')
      save(`${kind}OpenRegistrar`, receipt.contractAddress)
    }
    const registrar = getAddress(state[`${kind}OpenRegistrar`])
    if (
      !(await pub.readContract({
        address: registry,
        abi: registryAbi,
        functionName: 'hasRootRoles',
        args: [ROLE_REGISTRAR, registrar],
      }))
    ) {
      await send(`grant registrar on ${kind}`, {
        address: registry,
        abi: registryAbi,
        functionName: 'grantRootRoles',
        args: [ROLE_REGISTRAR, registrar],
      })
    }
    if (
      await pub.readContract({
        address: registry,
        abi: registryAbi,
        functionName: 'hasRootRoles',
        args: [ROLE_REGISTRAR, account.address],
      })
    ) {
      await send(`drop deployer roles on ${kind}`, {
        address: registry,
        abi: registryAbi,
        functionName: 'revokeRootRoles',
        args: [ALL_ROLES, account.address],
      })
    }
  }
  save('name', `${label}.eth`)
  console.log('done')
}

await main()
