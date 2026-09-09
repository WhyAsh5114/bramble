// Client-side payment config for docs/adr/0007. All optional at the process
// level — a node only talking to unmetered relays never calls the route
// these back, and requiredEnv below only fires when it's actually used.
function requiredEnv(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required to pay for a rendezvous token (sidecar config)`)
  return value
}

// The relay-sidecar instance this node pays for candidate exchange — see
// relay-sidecar/src/index.ts's POST /rendezvous-token.
export function rendezvousPaymentURL(): string {
  return requiredEnv('BRAMBLE_RENDEZVOUS_PAYMENT_URL')
}

export function hederaClientAccountID(): string {
  return requiredEnv('HEDERA_CLIENT_ACCOUNT_ID')
}

export function hederaClientPrivateKey(): string {
  return requiredEnv('HEDERA_CLIENT_PRIVATE_KEY')
}
