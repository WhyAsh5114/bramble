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

// Mirrors sidecar/src/payments/config.ts's HEDERA_USDC_ASSET_ID -- same
// "duplicated constant, no shared package" discipline as this dashboard's
// duplicated types (see lib/types.ts's own comment). This is the one asset
// this project's payment flows ever move, so token transfers matching any
// other asset ID are noise from the account's other testnet activity, not
// a bramble payment, and get filtered out.
export const HEDERA_USDC_ASSET_ID = '0.0.429274'

// Hedera account IDs to show settlement history for -- typically the
// relay operator's payee account(s) from `relay -payee <id>`. Comma-
// separated, read at request time like deviceLabels() above. Public
// mirror-node data either way: this only controls which accounts' history
// this dashboard bothers displaying, not what's fetchable.
export function hederaWatchAccountIds(): string[] {
  const raw = process.env.HEDERA_WATCH_ACCOUNT_IDS ?? ''
  return raw
    .split(',')
    .map((id) => id.trim())
    .filter(Boolean)
}

export const HEDERA_MIRROR_NODE_URL = 'https://testnet.mirrornode.hedera.com'
