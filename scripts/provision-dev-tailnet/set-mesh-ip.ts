// Sets (or clears) a device's `mesh-ip` text record on its already-deployed
// resolver — the same shape as set-pubkey.ts, generalized to the mesh-ip
// record docs/adr/0009 adds. Exists for two cases enroll.ts's own
// allocation step doesn't cover: backfilling a device enrolled before
// mesh-ip existed, and manually assigning a specific address rather than
// letting enroll.ts pick the lowest free one (e.g. reproducing a demo
// script's exact documented IPs).
//
// Usage: bun run set-mesh-ip.ts <device-label> <new-mesh-ip-or-empty>
//   bun run set-mesh-ip.ts device2 ""              # clear
//   bun run set-mesh-ip.ts device2 10.77.0.3/24     # assign
import { existsSync, readFileSync } from 'node:fs'
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
    if (match && !process.env[match[1].trim()]) {
      // Strip one layer of matching surrounding quotes — see
      // admincli/src/setup.ts's identical loader for the full reasoning.
      const rawValue = match[2].trim()
      process.env[match[1].trim()] = rawValue.replace(/^(['"])(.*)\1$/, '$2')
    }
  }
}

const [deviceLabel, newMeshIP] = process.argv.slice(2)
if (deviceLabel === undefined || newMeshIP === undefined) {
  throw new Error('usage: bun run set-mesh-ip.ts <device-label> <new-mesh-ip-or-empty>')
}

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
const tailnetName = fixture.tailnetName as string

const fullname = normalize(`${deviceLabel}.${tailnetName}`)
const dnsName = toHex(packetToBytes(fullname))

const resolverAddress = await publicClient.getEnsResolver({ name: fullname })
if (!resolverAddress) throw new Error(`no resolver found for ${fullname}`)
const resolverAbi = parseAbi(['function setText(bytes name, string key, string value)'])

const before = await publicClient.getEnsText({ name: fullname, key: 'mesh-ip' })
console.log(`current mesh-ip for ${fullname}: ${before ?? '(unset)'}`)

console.log(`\nsetting mesh-ip to ${newMeshIP === '' ? '(empty — clearing)' : newMeshIP}...`)
const hash = await wallet.writeContract({
  address: resolverAddress,
  abi: resolverAbi,
  functionName: 'setText',
  args: [dnsName, 'mesh-ip', newMeshIP],
})
console.log(`   tx: ${hash}`)
const receipt = await publicClient.waitForTransactionReceipt({ hash })
if (receipt.status !== 'success') throw new Error('setText reverted on-chain')

const after = await publicClient.getEnsText({ name: fullname, key: 'mesh-ip' })
if ((after ?? '') !== newMeshIP) throw new Error(`mesh-ip did not round-trip: got ${JSON.stringify(after)}`)

console.log(`\ndone. ${fullname}'s mesh-ip is now ${after || '(unset)'}.`)
