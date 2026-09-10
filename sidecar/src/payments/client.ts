// Pays a relay-sidecar's x402-gated /rendezvous-token route (docs/adr/0007)
// using the exact pattern scripts/gate0.4-blocky402-check/client.mjs already
// proved end to end (Gate 0.4) — @x402/fetch's automatic 402 -> sign -> retry
// loop over a real Hedera ECDSA signer.
import { wrapFetchWithPaymentFromConfig, decodePaymentResponseHeader } from '@x402/fetch'
import { createClientHederaSigner, PrivateKey } from '@x402/hedera'
import { ExactHederaScheme } from '@x402/hedera/exact/client'
import { hederaClientAccountID, hederaClientPrivateKey } from './config'

export type RendezvousTokenResult = {
  token: string
  expiresAt: number
  settlementTxId?: string
}

// payForRendezvousToken performs one real payment to sidecarUrl and returns
// the minted token. Each relay in a rendezvous set mints tokens under its
// own process-local secret (relay/sidecar.go generates a fresh random one
// per relay process) and verifies hellos against that same secret
// (relay/token.go) — a token bought here is only ever valid at the relay
// whose sidecar minted it, so sidecarUrl isn't optional or defaultable the
// way it might look; the caller (brambled) must re-call this once per relay
// it tries, mirroring payForDataRelaySession's per-call sidecarUrl exactly
// (this was previously read from a fixed BRAMBLE_RENDEZVOUS_PAYMENT_URL env
// var, which is what made Gate 4.3's failover unable to buy a token valid
// for a backup relay once the primary died — see docs/adr/0008).
//
// Throws if credentials are missing (requiredEnv in ./config) or the
// payment/settlement fails — the caller (routes/payments.ts) turns that into
// an HTTP error; brambled's own caller (brambled/main.go) already treats a
// failed RendezvousToken call as non-fatal for an otherwise-unmetered flow.
export async function payForRendezvousToken(sidecarUrl: string): Promise<RendezvousTokenResult> {
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

  const response = await fetchWithPayment(`${sidecarUrl.replace(/\/$/, '')}/rendezvous-token`, { method: 'POST' })
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
