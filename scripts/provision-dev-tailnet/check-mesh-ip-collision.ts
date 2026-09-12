// Quick pre-flight check for demo recording day: verifies a freshly
// enrolled device's mesh-ip doesn't collide with the other devices actually
// in play for this take. Exists because enroll.ts's allocation logic scans
// LabelRegistered history via getLogs, and the public RPC endpoint this
// repo defaults to is a load-balanced pool whose backends can silently
// return an incomplete scan (missing older registrations) with no error —
// verified live, Sept 12-13 2026, causing real mesh-ip collisions on
// several enrollments in a row despite the allocation logic itself being
// correct (docs/adr/0009, docs/adr/0010's RPC-reliability sections).
//
// Unlike enroll.ts's allocation, this only resolves the specific labels you
// name directly (no getLogs scan at all), so it isn't exposed to that same
// failure mode — a handful of direct getEnsText reads is far more reliable
// than a historical log scan against a flaky backend.
//
// Usage: bun run check-mesh-ip-collision.ts <label1> <label2> [label3 ...]
//   bun run check-mesh-ip-collision.ts worker-take-03 vps-demo agent-1
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createPublicClient, http, getAddress } from 'viem'
import { sepolia } from 'viem/chains'
import { normalize } from 'viem/ens'
import { UNIVERSAL_RESOLVER, RPC_URL, tailnetName } from '../../sidecar/src/ens/config'

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

const labels = process.argv.slice(2)
if (labels.length < 2) {
  throw new Error('usage: bun run check-mesh-ip-collision.ts <label1> <label2> [label3 ...] — name every device actually in play for this take')
}

const hackathonSepolia = {
  ...sepolia,
  contracts: { ...sepolia.contracts, ensUniversalResolver: { address: getAddress(UNIVERSAL_RESOLVER) } },
}
const publicClient = createPublicClient({ chain: hackathonSepolia, transport: http(RPC_URL) })

const meshIPs = await Promise.all(
  labels.map(async (label) => {
    const fullname = normalize(`${label}.${tailnetName()}`)
    const meshIP = await publicClient.getEnsText({ name: fullname, key: 'mesh-ip' })
    return { label, meshIP }
  })
)

for (const { label, meshIP } of meshIPs) {
  console.log(`${label.padEnd(24)} ${meshIP ?? '(no mesh-ip record)'}`)
}

const seen = new Map<string, string>()
let collision = false
for (const { label, meshIP } of meshIPs) {
  if (!meshIP) continue
  const existing = seen.get(meshIP)
  if (existing) {
    console.log(`\nCOLLISION: ${existing} and ${label} both resolve to ${meshIP}`)
    console.log(`fix with: bun run set-mesh-ip.ts ${label} <a free 10.77.0.x/24 not listed above>`)
    collision = true
  }
  seen.set(meshIP, label)
}

if (!collision) {
  console.log('\nno collisions — safe to proceed')
}
