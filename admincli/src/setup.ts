// Shared bootstrap for every admincli command: .env loading, the
// hackathon-Sepolia chain override, and a public client. Mirrors the
// boilerplate already duplicated across scripts/gate0.3-eac-check and
// scripts/provision-dev-tailnet — kept here once since every file in this
// package needs exactly the same setup, unlike those cross-package scripts.
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createPublicClient, http, getAddress, type Address } from 'viem'
import { sepolia } from 'viem/chains'
import { privateKeyToAccount } from 'viem/accounts'
import { UNIVERSAL_RESOLVER, RPC_URL } from '../../sidecar/src/ens/config'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const envPath = path.resolve(__dirname, '../../.env')
if (existsSync(envPath)) {
  const env = await Bun.file(envPath).text()
  for (const line of env.split('\n')) {
    const match = line.match(/^([^#=]+)=(.*)$/)
    if (match && !process.env[match[1].trim()]) {
      // Strip one layer of matching surrounding quotes — every other
      // env-loading path in this repo does this for free (shell `source`,
      // Vite/vitest, Next.js), but this hand-rolled parser is a regex
      // split with no quote handling at all, so a quoted .env value (e.g.
      // BRAMBLE_TAILNET_NAME="foo.eth") previously landed in process.env
      // with the literal quote characters still attached — silently
      // breaking anything sensitive to the exact string (ENS name
      // normalization, hex key parsing).
      const rawValue = match[2].trim()
      process.env[match[1].trim()] = rawValue.replace(/^(['"])(.*)\1$/, '$2')
    }
  }
}

// viem ships a built-in Universal Resolver address for Sepolia that points
// at the production/beta ENSv2 deployment, not the hackathon one — see
// docs/04_TECH_STACK.md, "Universal Resolver gotcha."
export const hackathonSepolia = {
  ...sepolia,
  contracts: { ...sepolia.contracts, ensUniversalResolver: { address: getAddress(UNIVERSAL_RESOLVER) } },
}

export const publicClient = createPublicClient({ chain: hackathonSepolia, transport: http(RPC_URL) })

// accountFromEnv resolves a private key from the named env var into a viem
// Account. Defaults to SEPOLIA_PRIVATE_KEY (the operator's own key, same var
// scripts/provision-dev-tailnet and scripts/gate0.3-eac-check use); Gate
// 2.1's test passes distinct env vars for its enroller/rotator/revoker/
// no-role test accounts.
export function accountFromEnv(envVar = 'SEPOLIA_PRIVATE_KEY') {
  const raw = process.env[envVar]
  if (!raw) throw new Error(`${envVar} not found in .env`)
  const privateKey = (raw.trim().startsWith('0x') ? raw.trim() : `0x${raw.trim()}`) as `0x${string}`
  return privateKeyToAccount(privateKey)
}

export type { Address }
