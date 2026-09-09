import { Hono } from 'hono'
import { paymentMiddleware, x402ResourceServer } from '@x402/hono'
import { HTTPFacilitatorClient } from '@x402/core/server'
import { ExactHederaScheme } from '@x402/hedera/exact/server'
import { PORT, tokenSecret, payTo, USDC_ASSET_ID, PRICE_ATOMIC, BLOCKY402_FACILITATOR_URL } from './config'
import { mintToken } from './token'

const facilitatorClient = new HTTPFacilitatorClient({ url: BLOCKY402_FACILITATOR_URL })
const resourceServer = new x402ResourceServer(facilitatorClient).register(
  'hedera:testnet',
  new ExactHederaScheme({
    defaultAssets: { 'hedera:testnet': { asset: USDC_ASSET_ID, decimals: 6 } },
  })
)

const app = new Hono()

app.get('/health', (c) => c.json({ ok: true }))

app.use(
  paymentMiddleware(
    {
      'POST /rendezvous-token': {
        accepts: {
          scheme: 'exact',
          network: 'hedera:testnet',
          payTo: payTo(),
          price: { asset: USDC_ASSET_ID, amount: PRICE_ATOMIC },
        },
        description: 'One rendezvous candidate-exchange session (docs/adr/0007)',
      },
    },
    resourceServer
  )
)

app.post('/rendezvous-token', (c) => {
  const minted = mintToken(tokenSecret())
  return c.json(minted)
})

export default {
  port: PORT,
  // Deliberately NOT loopback-only, unlike the ENS sidecar — this route is
  // the relay's public payment surface, same reachability as the relay's
  // own TCP port (docs/03_ARCHITECTURE.md, "Relays are the deliberate
  // exception"). Token verification itself stays local to the Go relay
  // (docs/adr/0007) — this process never needs to be trusted with anything
  // beyond "did a real payment settle."
  fetch: app.fetch,
}

console.log(`relay-sidecar listening on :${PORT}`)
