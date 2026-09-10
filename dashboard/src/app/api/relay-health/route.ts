import { NextRequest } from 'next/server'
import { canonicalRelayBase, discoveredRelays } from '@/lib/sidecar-server'

// Same discipline as relay-price/route.ts: the target is chain-discovered,
// not caller-supplied, so it's restricted to exactly the one path
// (relay-sidecar's GET /health) and to bases that actually appear in the
// current discovered relay set, never an arbitrary url query value.
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
    const approved =
      relays.dataRelays.some((relay) => {
        try {
          return canonicalRelayBase(relay.sidecarUrl) === requestedBase
        } catch {
          return false
        }
      }) ||
      relays.rendezvous.some((relay) => {
        if (!relay.sidecarUrl) return false
        try {
          return canonicalRelayBase(relay.sidecarUrl) === requestedBase
        } catch {
          return false
        }
      })
    if (!approved) {
      return Response.json({ error: 'relay is not in the discovered relay set' }, { status: 403 })
    }

    const res = await fetch(new URL('health', requestedBase), {
      cache: 'no-store',
      redirect: 'error',
      signal: AbortSignal.timeout(5_000),
    })
    return Response.json({ reachable: res.ok }, { status: 200 })
  } catch {
    return Response.json({ reachable: false }, { status: 200 })
  }
}
