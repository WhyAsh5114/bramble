import { Hono } from 'hono'
import { payForRendezvousToken } from '../payments/client'
import { payForDataRelaySession } from '../payments/datarelay'

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

// brambled calls this once per peer flagged with -relay (docs/adr/0008),
// after it has already picked which data relay to use. sidecarUrl/bytes
// come from brambled per call, not env, since which relay to pay varies by
// connection — see payments/datarelay.ts's doc comment.
paymentsRoute.post('/data-relay-session', async (c) => {
  const { sidecarUrl, bytes } = await c.req.json<{ sidecarUrl: string; bytes: number }>()
  try {
    const result = await payForDataRelaySession(sidecarUrl, bytes)
    return c.json(result)
  } catch (err) {
    return c.json({ error: err instanceof Error ? err.message : String(err) }, 502)
  }
})
