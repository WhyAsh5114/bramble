// Sets (or clears) a device's `pubkey` text record on its already-deployed
// resolver. Generalizes register-device2.ts's step-3 setText() call into a
// standalone, reusable tool — used both to revoke (pass an empty string) and
// to restore (pass the saved value back) for Gate 1.3's revocation-timing
// runbook (see brambled/README.md). Prints the current value before
// overwriting it, so a revoke run can always be undone.
//
// Usage: bun run set-pubkey.ts <device-label> <new-pubkey-or-empty>
//   bun run set-pubkey.ts device2 ""                    # revoke
//   bun run set-pubkey.ts device2 76158fe000ffdcc4...    # restore
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
    if (match && !process.env[match[1].trim()]) process.env[match[1].trim()] = match[2].trim()
  }
}

const [deviceLabel, newPubkey] = process.argv.slice(2)
if (deviceLabel === undefined || newPubkey === undefined) {
  throw new Error('usage: bun run set-pubkey.ts <device-label> <new-pubkey-or-empty>')
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

const before = await publicClient.getEnsText({ name: fullname, key: 'pubkey' })
console.log(`current pubkey for ${fullname}: ${before ?? '(unset)'}`)
if (before) {
  console.log(`(save this if you intend to restore it later: bun run set-pubkey.ts ${deviceLabel} ${before})`)
}

console.log(`\nsetting pubkey to ${newPubkey === '' ? '(empty — revoking)' : newPubkey}...`)
const startedAt = Date.now()
const hash = await wallet.writeContract({
  address: resolverAddress,
  abi: resolverAbi,
  functionName: 'setText',
  args: [dnsName, 'pubkey', newPubkey],
})
console.log(`   tx: ${hash}`)
const receipt = await publicClient.waitForTransactionReceipt({ hash })
if (receipt.status !== 'success') throw new Error('setText reverted on-chain')
const confirmedAt = Date.now()
console.log(`   confirmed at ${new Date(confirmedAt).toISOString()} (${confirmedAt - startedAt}ms after submit)`)

const after = await publicClient.getEnsText({ name: fullname, key: 'pubkey' })
if ((after ?? '') !== newPubkey) throw new Error(`pubkey did not round-trip: got ${JSON.stringify(after)}`)

console.log(`\ndone. ${fullname}'s pubkey is now ${after || '(unset)'}.`)
