import type { ActivityEvent, PeerStatus, PingResponse } from '@/lib/types'

const requestTimeoutMs = 5_000

// Base URL of one node's own brambled status API (brambled/statusapi) —
// loopback-only on that node, same trust model as SIDECAR_URL in
// sidecar-server.ts. Not the same process as the ENS sidecar: this is
// brambled's own runtime state (live WireGuard peers, ping, recent
// admission/gateway activity), which the ENS sidecar has no visibility
// into.
function brambledBaseUrl(): URL {
  const base = new URL(process.env.BRAMBLED_STATUS_URL ?? 'http://127.0.0.1:7899')
  if (base.protocol !== 'http:' && base.protocol !== 'https:') {
    throw new Error('BRAMBLED_STATUS_URL must use http(s)')
  }
  return base
}

async function fetchBrambled(path: string, init?: RequestInit): Promise<Response> {
  return fetch(new URL(path, brambledBaseUrl()), {
    cache: 'no-store',
    redirect: 'error',
    signal: AbortSignal.timeout(requestTimeoutMs),
    ...init,
  })
}

export async function proxyBrambledGet(path: string): Promise<Response> {
  try {
    const response = await fetchBrambled(path)
    const body = await response.text()
    return new Response(body, {
      status: response.status,
      headers: { 'Content-Type': response.headers.get('content-type') ?? 'application/json' },
    })
  } catch (err) {
    return Response.json({ error: err instanceof Error ? err.message : String(err) }, { status: 502 })
  }
}

export async function trackedPeers(): Promise<PeerStatus[]> {
  const response = await fetchBrambled('/peers')
  if (!response.ok) throw new Error(`brambled status /peers returned ${response.status}`)
  return response.json() as Promise<PeerStatus[]>
}

export async function recentActivity(): Promise<ActivityEvent[]> {
  const response = await fetchBrambled('/activity')
  if (!response.ok) throw new Error(`brambled status /activity returned ${response.status}`)
  return response.json() as Promise<ActivityEvent[]>
}

export async function pingPeer(label: string): Promise<PingResponse> {
  const response = await fetchBrambled('/ping', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label }),
  })
  if (!response.ok) throw new Error(`brambled status /ping returned ${response.status}`)
  return response.json() as Promise<PingResponse>
}
