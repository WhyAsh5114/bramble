import { NextRequest } from 'next/server'
import { canonicalRelayBase, discoveredRelays } from '@/lib/sidecar-server'

// Data relays are discovered at runtime from sidecar's /relays (each with its
// own sidecarUrl), so unlike sidecar itself this destination can't be a
// static next.config.ts rewrite — it varies per relay. This route is the one
// place the dashboard proxies to an arbitrary, chain-discovered URL, so it's
// restricted to exactly the one path (relay-sidecar's GET /price) rather than
// forwarding an arbitrary path segment.
export async function GET(req: NextRequest) {
  const sidecarUrl = req.nextUrl.searchParams.get('url')
  if (!sidecarUrl) {
    return Response.json({ error: 'missing url query param' }, { status: 400 })
  }

  let requestedBase: string
  try {
    requestedBase = canonicalRelayBase(sidecarUrl)
  } catch {
    return Response.json({ error: 'invalid url query param' }, { status: 400 })
  }

  try {
    const relays = await discoveredRelays()
    const approved = relays.dataRelays.some((relay) => {
      try {
        return canonicalRelayBase(relay.sidecarUrl) === requestedBase
      } catch {
        return false
      }
    })
    if (!approved) {
      return Response.json({ error: 'relay is not in the discovered data-relay set' }, { status: 403 })
    }

    const res = await fetch(new URL('price', requestedBase), {
      cache: 'no-store',
      redirect: 'error',
      signal: AbortSignal.timeout(5_000),
    })
    const body = await res.json()
    return Response.json(body, { status: res.status })
  } catch (err) {
    return Response.json({ error: err instanceof Error ? err.message : String(err) }, { status: 502 })
  }
}
