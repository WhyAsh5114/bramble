// Client-side payment config for docs/adr/0007. All optional at the process
// level — a node only talking to unmetered relays never calls the route
// these back, and requiredEnv below only fires when it's actually used.
function requiredEnv(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required to pay for a rendezvous token (sidecar config)`)
  return value
}

export function hederaClientAccountID(): string {
  return requiredEnv('HEDERA_CLIENT_ACCOUNT_ID')
}

export function hederaClientPrivateKey(): string {
  return requiredEnv('HEDERA_CLIENT_PRIVATE_KEY')
}

// The client must opt testnet USDC into x402's spend controls explicitly:
// Hedera asset IDs are not in the SDK's default-asset table. Keep a finite
// per-payment ceiling even for testnet so a bad or compromised relay cannot
// choose an arbitrary amount in its 402 response. 100,000 atomic units is
// 0.10 USDC; larger intentional demos can raise the env value explicitly.
export const HEDERA_USDC_ASSET_ID = '0.0.429274'

export function hederaMaxPaymentAtomic(): string {
  const value = process.env.HEDERA_MAX_PAYMENT_ATOMIC ?? '100000'
  if (!/^\d+$/.test(value) || BigInt(value) <= 0n) {
    throw new Error('HEDERA_MAX_PAYMENT_ATOMIC must be a positive integer in atomic USDC units')
  }
  return value
}
