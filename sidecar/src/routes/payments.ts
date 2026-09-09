import { Hono } from 'hono'
import { payForRendezvousToken } from '../payments/client'

export const paymentsRoute = new Hono()

// brambled calls this (loopback only, same as every other sidecar route —
// see index.ts) immediately before each rendezvous.Exchange call. The real
// x402/Hedera payment happens inside payForRendezvousToken; this route is
// just the local HTTP boundary brambled already crosses for everything else
// (docs/adr/0002, generalized to payments by docs/adr/0007).
paymentsRoute.post('/rendezvous-token', async (c) => {
  try {
    const result = await payForRendezvousToken()
    return c.json({ token: result.token, expiresAt: result.expiresAt, settlementTxId: result.settlementTxId })
  } catch (err) {
    return c.json({ error: err instanceof Error ? err.message : String(err) }, 502)
  }
})
