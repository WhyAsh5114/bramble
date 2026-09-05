// One-off fixup: the original provisioning run wrote a placeholder string
// ("bramble-dev-tailnet-test-wireguard-pubkey") as the pubkey text record —
// fine for proving ENS resolution plumbing (Section A), but Section B's
// admission loop hex-decodes this value as a real WireGuard public key, and
// a placeholder string isn't valid hex. This writes a real generated
// WireGuard public key to the same already-deployed device resolver and
// updates the fixture to match.
//
// Usage: bun run fix-pubkey.ts <device-resolver-address> <new-pubkey-hex>
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createPublicClient, createWalletClient, http, parseAbi, getAddress, toHex } from 'viem'
import { sepolia } from 'viem/chains'
import { privateKeyToAccount } from 'viem/accounts'
import { normalize, packetToBytes } from 'viem/ens'
import { UNIVERSAL_RESOLVER, RPC_URL } from '../../sidecar/src/ens/config'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const envPath = path.resolve(__dirname, '../../.env')
if (existsSync(envPath)) {
  const env = await Bun.file(envPath).text()
  for (const line of env.split('\n')) {
    const match = line.match(/^([^#=]+)=(.*)$/)
    if (match && !process.env[match[1].trim()]) process.env[match[1].trim()] = match[2].trim()
  }
}

const [deviceResolverArg, newPubkey] = process.argv.slice(2)
if (!deviceResolverArg || !newPubkey) {
  throw new Error('usage: bun run fix-pubkey.ts <device-resolver-address> <new-pubkey-hex>')
}
const deviceResolverAddress = getAddress(deviceResolverArg)

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

const fixturePath = path.resolve(__dirname, '../../sidecar/test/fixtures/dev-tailnet.json')
const fixture = JSON.parse(readFileSync(fixturePath, 'utf-8'))
const deviceFullName = fixture.deviceFullname as string
const normalizedDeviceName = normalize(deviceFullName)
const dnsNameDevice = toHex(packetToBytes(normalizedDeviceName))

console.log(`Writing pubkey for ${deviceFullName} on resolver ${deviceResolverAddress}...`)
const hash = await wallet.writeContract({
  address: deviceResolverAddress,
  abi: resolverAbi,
  functionName: 'setText',
  args: [dnsNameDevice, 'pubkey', newPubkey],
})
console.log(`tx: ${hash}`)
const receipt = await publicClient.waitForTransactionReceipt({ hash })
if (receipt.status !== 'success') throw new Error('setText reverted on-chain')

const readBack = await publicClient.getEnsText({ name: normalizedDeviceName, key: 'pubkey' })
if (readBack !== newPubkey) throw new Error(`pubkey did not round-trip: got "${readBack}"`)

fixture.expectedPubkey = newPubkey
writeFileSync(fixturePath, JSON.stringify(fixture, null, 2) + '\n')
console.log(`Fixture updated: ${fixturePath}`)
