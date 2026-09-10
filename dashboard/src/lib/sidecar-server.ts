import type { Relays } from '@/lib/types'

const requestTimeoutMs = 5_000

function sidecarBaseUrl(): URL {
  const base = new URL(process.env.SIDECAR_URL ?? 'http://127.0.0.1:7890')
  if (base.protocol !== 'http:' && base.protocol !== 'https:') {
    throw new Error('SIDECAR_URL must use http(s)')
  }
  return base
}

export async function fetchSidecar(path: string): Promise<Response> {
  return fetch(new URL(path, sidecarBaseUrl()), {
    cache: 'no-store',
    redirect: 'error',
    signal: AbortSignal.timeout(requestTimeoutMs),
  })
}

export async function proxySidecarGet(path: string): Promise<Response> {
  try {
    const response = await fetchSidecar(path)
    const body = await response.text()
    return new Response(body, {
      status: response.status,
      headers: { 'Content-Type': response.headers.get('content-type') ?? 'application/json' },
    })
  } catch (err) {
    return Response.json({ error: err instanceof Error ? err.message : String(err) }, { status: 502 })
  }
}

export async function discoveredRelays(): Promise<Relays> {
  const response = await fetchSidecar('/relays')
  if (!response.ok) throw new Error(`sidecar /relays returned ${response.status}`)
  return response.json() as Promise<Relays>
}

export function canonicalRelayBase(raw: string): string {
  const url = new URL(raw)
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error('relay URL must use http(s)')
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new Error('relay URL must not contain credentials, a query, or a fragment')
  }
  url.pathname = `${url.pathname.replace(/\/+$/, '')}/`
  return url.toString()
}
