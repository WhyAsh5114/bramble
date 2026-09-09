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
