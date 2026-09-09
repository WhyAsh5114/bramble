// Config for the rendezvous relay's payment sidecar. See docs/adr/0007 for
// why this is a separate small service rather than a route bolted onto the
// per-node ENS sidecar: this one belongs to the relay operator, not a node.
export const PORT = Number(process.env.PORT || 7891)

function requiredEnv(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required (relay-sidecar config)`)
  return value
}

// Hex-encoded, generated fresh by the spawning `relay -meter` process at its
// own startup and handed over via env — never operator-configured, never
// touches ENS or any chain. Purely an anti-freeloading measure for the
// relay's own token minting; orthogonal to the mesh's admission model
// (docs/adr/0007, "Rationale").
export function tokenSecret(): string {
  return requiredEnv('RENDEZVOUS_TOKEN_SECRET')
}

export function payTo(): string {
  return requiredEnv('HEDERA_RELAY_OPERATOR_ACCOUNT_ID')
}

// Testnet USDC (0.0.429274, 6 decimals) — docs/adr/0007's settlement-asset
// decision. 0.01 USDC per rendezvous token.
export const USDC_ASSET_ID = '0.0.429274'
export const PRICE_ATOMIC = '10000' // 0.01 USDC

export const BLOCKY402_FACILITATOR_URL = 'https://api.testnet.blocky402.com'

// Matches docs/adr/0007's token wire format exactly — the Go relay's
// verifier (relay/token.go) must stay byte-for-byte compatible with this.
export const TOKEN_TTL_SECONDS = 60

// Data-plane relay (docs/adr/0008) — all optional-by-default, set only when
// the spawning `relay -data-relay` flag is on. dataRelayEnabled() gates
// whether index.ts mounts the /data-relay-session and /price routes at all.
export function dataRelayEnabled(): boolean {
  return process.env.DATA_RELAY_ENABLED === '1'
}

// Loopback URL of the Go relay's allocation API (relay/datarelay.go) — set
// by the same spawning process that generates RENDEZVOUS_TOKEN_SECRET.
export function internalAPIURL(): string {
  return requiredEnv('DATA_RELAY_INTERNAL_API_URL')
}

// Atomic USDC per byte forwarded — DynamicPrice input for pricing.ts.
export function pricePerByteAtomic(): bigint {
  return BigInt(requiredEnv('DATA_RELAY_PRICE_PER_BYTE'))
}
