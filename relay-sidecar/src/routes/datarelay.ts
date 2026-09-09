// Data-plane relay routes (docs/adr/0008): POST /data-relay-session is
// x402-gated with a DynamicPrice computed from the requested byte count
// (?bytes=N), so one route covers every allotment size instead of fixed
// tiers. GET /price is an unauthenticated quote used by brambled's
// relay-selection logic (adr/0008, "Relay selection").
import { Hono } from 'hono'
import type { HTTPRequestContext } from '@x402/core/server'
import { internalAPIURL } from '../config'
import { priceForBytes } from '../pricing'

function parseBytes(raw: string | string[] | undefined): number {
  const value = Array.isArray(raw) ? raw[0] : raw
  const n = Number(value)
  if (!value || !Number.isInteger(n) || n <= 0) {
    throw new Error(`bytes query param must be a positive integer, got ${value}`)
  }
  return n
}

// dataRelayPrice is the DynamicPrice function registered in index.ts's
// paymentMiddleware routes config — x402 calls this to compute what the
// client owes before any payment is verified.
export function dataRelayPrice(context: HTTPRequestContext) {
  const bytes = parseBytes(context.adapter.getQueryParam?.('bytes'))
  return priceForBytes(bytes)
}

export const dataRelayRoute = new Hono()

// Runs only after paymentMiddleware has verified and settled payment for
// this exact request — re-parsing ?bytes here must match dataRelayPrice's
// parse exactly, since it decides how big a session to actually allocate.
dataRelayRoute.post('/data-relay-session', async (c) => {
  let bytes: number
  try {
    bytes = parseBytes(c.req.query('bytes'))
  } catch (err) {
    return c.json({ error: err instanceof Error ? err.message : String(err) }, 400)
  }

  const res = await fetch(`${internalAPIURL()}/internal/data-relay-session`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ bytes }),
  })
  if (!res.ok) {
    return c.json({ error: `relay allocation failed: HTTP ${res.status}` }, 502)
  }
  return c.json(await res.json())
})

// Unauthenticated price quote — brambled's -data-relay selection queries
// this on every candidate relay to compare price (and, via round-trip
// time, a latency proxy) before buying a session from the cheapest one.
dataRelayRoute.get('/price', (c) => {
  return c.json(priceForBytes(1))
})
