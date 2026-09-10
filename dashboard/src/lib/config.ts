// Server-only config, read at request time (not NEXT_PUBLIC_*, not inlined
// at build time) — so which devices this dashboard watches can change
// without a rebuild, which matters when demo topology gets adjusted last
// minute. Read through /api/config, never imported into client components
// directly.
export function deviceLabels(): string[] {
  const raw = process.env.DEVICE_LABELS ?? ''
  return raw
    .split(',')
    .map((l) => l.trim())
    .filter(Boolean)
}

// Base URL of the hackathon's own ENSv2 explorer (docs/04_TECH_STACK.md) —
// the production app.ens.domains doesn't know this deployment's contracts,
// so linking there would resolve against the wrong registry entirely.
export const ENS_EXPLORER_URL = 'https://hackathon-deployment-portal-app.ens-cf.workers.dev/'

export const SEPOLIA_ETHERSCAN_URL = 'https://sepolia.etherscan.io'
export const HASHSCAN_TESTNET_URL = 'https://hashscan.io/testnet'
