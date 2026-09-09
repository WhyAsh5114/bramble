// Pays the relay-sidecar's x402-gated /rendezvous-token route (docs/adr/0007)
// using the exact pattern scripts/gate0.4-blocky402-check/client.mjs already
// proved end to end (Gate 0.4) — @x402/fetch's automatic 402 -> sign -> retry
// loop over a real Hedera ECDSA signer.
import { wrapFetchWithPaymentFromConfig, decodePaymentResponseHeader } from '@x402/fetch'
import { createClientHederaSigner, PrivateKey } from '@x402/hedera'
import { ExactHederaScheme } from '@x402/hedera/exact/client'
import { rendezvousPaymentURL, hederaClientAccountID, hederaClientPrivateKey } from './config'

export type RendezvousTokenResult = {
  token: string
  expiresAt: number
  settlementTxId?: string
}

// payForRendezvousToken performs one real payment and returns the minted
// token. Throws if credentials are missing (requiredEnv in ./config) or the
// payment/settlement fails — the caller (routes/payments.ts) turns that into
// an HTTP error; brambled's own caller (brambled/main.go) already treats a
// failed RendezvousToken call as non-fatal for an otherwise-unmetered flow.
export async function payForRendezvousToken(): Promise<RendezvousTokenResult> {
  const signer = createClientHederaSigner(
    hederaClientAccountID(),
    PrivateKey.fromStringECDSA(hederaClientPrivateKey()),
    { network: 'hedera:testnet' }
  )

  const fetchWithPayment = wrapFetchWithPaymentFromConfig(fetch, {
    schemes: [{ network: 'hedera:testnet', client: new ExactHederaScheme(signer) }],
    // Settling in an HTS token (testnet USDC, docs/adr/0007), which the
    // client's default spend-control allowlist doesn't recognize as a
    // Hedera "default asset" any more than native HBAR was in Gate 0.4 —
    // same reasoning as that script's client.mjs for disabling this.
    spendControls: false,
  })

  const response = await fetchWithPayment(`${rendezvousPaymentURL()}/rendezvous-token`, { method: 'POST' })
  if (!response.ok) {
    throw new Error(`relay-sidecar returned HTTP ${response.status} for /rendezvous-token`)
  }
  const body = (await response.json()) as { token: string; expiresAt: number }

  const paymentResponseHeader = response.headers.get('PAYMENT-RESPONSE')
  const settlementTxId = paymentResponseHeader
    ? decodePaymentResponseHeader(paymentResponseHeader).transaction
    : undefined

  return { token: body.token, expiresAt: body.expiresAt, settlementTxId }
}
