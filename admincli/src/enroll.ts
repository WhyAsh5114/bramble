// Enrolls a new device subname: deploys a dedicated Permissioned Resolver
// for it (mirrors scripts/gate0.3-eac-check / scripts/provision-dev-tailnet's
// proven pattern — "a subname with its own Permissioned Resolver"), calls
// register() on the tailnet's subregistry, then writes the initial pubkey.
//
// register() requires the caller to hold ROLE_REGISTRAR on the subregistry
// (root-scoped — see docs/adr/0004 and sidecar/src/ens/config.ts's
// ROLE_REGISTRAR comment); the resolver deploy grants ALL_ROLES on the new
// resolver to the deployer (this same caller), so no separate rotate/revoke
// grant is needed just to write the initial pubkey or mesh-ip — an operator
// running this command already deployed the resolver they're writing to.
//
// Also allocates and writes this device's mesh-ip (docs/adr/0009) — the
// operator, not the device, since a self-writable address would let a
// device steal another device's route (the same label-hijack failure mode
// docs/adr/0005's requester-binding fix closed for ACL digests). This
// replaces having to pass -peer label=allowed-ip/prefix by hand for every
// device brambled tracks.
//
// Usage: bun run enroll.ts <device-label> <pubkey> [expiry-years=10]
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
import { normalize, packetToBytes } from 'viem/ens'
import {
  RPC_URL,
  tailnetName,
  tailnetRegistry,
  VERIFIABLE_FACTORY,
  PERMISSIONED_RESOLVER_IMPL,
  ALL_ROLES,
  REGISTRATION_ROLE_BITMAP,
} from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { registryWriteAbi, resolverWriteAbi } from './roles'
import { connectLedgerAccount } from './ledger-eth-sign'
import { allocateMeshIP } from './mesh-ip'
import { listDeviceLabels } from '../../sidecar/src/ens/devices'

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

  // LEDGER_SIGN=1 signs all four writes below through a real Ledger,
  // via a from-scratch hw-app-eth signer (docs/adr/0001's Pivot section,
  // "Upgrade" note) -- wallet-cli's own `send` can't sign on Sepolia at
  // all (Gap 4, docs/12_LEDGER_DX_FEEDBACK.md). Defaults to the existing
  // SEPOLIA_PRIVATE_KEY/viem path so this is opt-in, not a behavior change
  // for every other caller (rotate.ts, revoke.ts, set-acl.ts) that still
  // needs software signing.
  const useLedger = process.env.LEDGER_SIGN === '1'
  const ledger = useLedger ? await connectLedgerAccount() : null
  const account = ledger ? ledger.account : accountFromEnv()
  if (useLedger) console.log(`signing as Ledger account ${account.address} -- confirm each transaction on-device`)
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  async function writeAndWait(label: string, args: Parameters<typeof wallet.writeContract>[0]) {
    const hash = await wallet.writeContract(args)
    console.log(`   tx (${label}): ${hash}`)
    const receipt = await publicClient.waitForTransactionReceipt({ hash })
    if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
    return receipt
  }

  try {
    console.log(`[1/4] Deploying a dedicated resolver for ${deviceLabel}...`)
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

    console.log(`\n[2/4] Registering ${deviceLabel}.${tailnetName()} (requires ROLE_REGISTRAR)...`)
    const farExpiry = BigInt(Math.floor(Date.now() / 1000) + expiryYears * 365 * 24 * 60 * 60)
    const zeroAddress = '0x0000000000000000000000000000000000000000' as const
    await writeAndWait('register', {
      address: tailnetRegistry(),
      abi: registryWriteAbi,
      functionName: 'register',
      args: [deviceLabel, account.address, zeroAddress, resolverAddress, REGISTRATION_ROLE_BITMAP, farExpiry],
    })

    console.log('\n[3/4] Writing initial pubkey...')
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

    console.log('\n[4/4] Allocating mesh-ip...')
    const otherLabels = (await listDeviceLabels()).filter((l) => l !== deviceLabel)
    const usedMeshIPs = await Promise.all(
      otherLabels.map((l) => publicClient.getEnsText({ name: normalize(`${l}.${tailnetName()}`), key: 'mesh-ip' }))
    )
    const meshIP = allocateMeshIP(usedMeshIPs)
    await writeAndWait('setText(mesh-ip)', {
      address: resolverAddress,
      abi: resolverWriteAbi,
      functionName: 'setText',
      args: [dnsName, 'mesh-ip', meshIP],
    })

    const meshIPReadBack = await publicClient.getEnsText({ name: fullname, key: 'mesh-ip' })
    if (meshIPReadBack !== meshIP) throw new Error(`mesh-ip did not round-trip: got ${JSON.stringify(meshIPReadBack)}`)

    console.log(
      `\ndone. ${fullname} enrolled, resolver ${resolverAddress}, pubkey ${readBack}, mesh-ip ${meshIPReadBack}.`
    )
  } finally {
    if (ledger) await ledger.close()
  }
}

main().catch((err) => {
  console.error('\nenroll failed:', err)
  process.exit(1)
})
