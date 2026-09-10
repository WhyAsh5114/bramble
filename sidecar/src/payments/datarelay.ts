// Pays for a data-relay session (docs/adr/0008), mirroring
// sidecar/src/payments/client.ts's payForRendezvousToken exactly — same
// @x402/fetch automatic 402 -> sign -> retry loop over the node's real
// Hedera ECDSA signer. The one difference: the target relay-sidecar's URL
// isn't fixed per node like BRAMBLE_RENDEZVOUS_PAYMENT_URL — brambled picks
// it per connection from its -data-relay pool (adr/0008, "Relay
// selection"), so it's passed in per call instead of read from env.
import { wrapFetchWithPaymentFromConfig, decodePaymentResponseHeader } from '@x402/fetch'
import { createClientHederaSigner, PrivateKey } from '@x402/hedera'
import { ExactHederaScheme } from '@x402/hedera/exact/client'
import { HEDERA_USDC_ASSET_ID, hederaClientAccountID, hederaClientPrivateKey, hederaMaxPaymentAtomic } from './config'

export type DataRelaySessionResult = {
  sessionId: string
  port: number
  expiresAt: number
  settlementTxId?: string
}

export async function payForDataRelaySession(sidecarUrl: string, bytes: number): Promise<DataRelaySessionResult> {
  if (!Number.isSafeInteger(bytes) || bytes <= 0) {
    throw new Error('data-relay session bytes must be a positive safe integer')
  }
  const signer = createClientHederaSigner(
    hederaClientAccountID(),
    PrivateKey.fromStringECDSA(hederaClientPrivateKey()),
    { network: 'hedera:testnet' }
  )

  const fetchWithPayment = wrapFetchWithPaymentFromConfig(fetch, {
    schemes: [{ network: 'hedera:testnet', client: new ExactHederaScheme(signer) }],
    spendControls: {
      allowedAssets: [
        {
          network: 'hedera:testnet',
          asset: HEDERA_USDC_ASSET_ID,
          maxAmountPerPayment: hederaMaxPaymentAtomic(),
        },
      ],
    },
  })

  const url = `${sidecarUrl.replace(/\/$/, '')}/data-relay-session?bytes=${bytes}`
  const response = await fetchWithPayment(url, { method: 'POST' })
  if (!response.ok) {
    throw new Error(`relay-sidecar returned HTTP ${response.status} for /data-relay-session`)
  }
  const body = (await response.json()) as { sessionId: string; port: number; expiresAt: number }

  const paymentResponseHeader = response.headers.get('PAYMENT-RESPONSE')
  const settlementTxId = paymentResponseHeader
    ? decodePaymentResponseHeader(paymentResponseHeader).transaction
    : undefined

  return { sessionId: body.sessionId, port: body.port, expiresAt: body.expiresAt, settlementTxId }
}
