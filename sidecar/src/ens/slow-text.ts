import type { EnsClient } from './client'
import { tailnetRegistry, fastResolveEnabled } from './config'

// Only routing metadata belongs here. In particular, pubkey stays fresh:
// clearing it is another revocation mechanism (ADR 0004).
type SlowKey = 'mesh-ip' | 'rendezvous-address' | 'sidecar-url'
const caches = new WeakMap<EnsClient, Map<string, { value: string; expires: number }>>()
const ttl = 60_000
const maxEntries = 1024

function cacheFor(client: EnsClient) {
  let cache = caches.get(client)
  if (!cache) {
    cache = new Map()
    caches.set(client, cache)
  }
  return cache
}

function cacheKey(name: string, key: SlowKey) {
  return `${tailnetRegistry()}:${name}:${key}`
}

export function cachedSlowText(client: EnsClient, name: string, key: SlowKey): string | undefined {
  if (!fastResolveEnabled()) return undefined
  const cache = cacheFor(client)
  const id = cacheKey(name, key)
  const entry = cache.get(id)
  if (!entry) return undefined
  if (Date.now() >= entry.expires) {
    cache.delete(id)
    return undefined
  }
  return entry.value
}

export function rememberSlowText(client: EnsClient, name: string, key: SlowKey, value: string | null) {
  // Do not cache absent records: enrollment should appear on the next read.
  if (!fastResolveEnabled() || !value) return
  const cache = cacheFor(client)
  if (cache.size >= maxEntries) cache.delete(cache.keys().next().value!)
  cache.set(cacheKey(name, key), { value, expires: Date.now() + ttl })
}

export async function readSlowText(client: EnsClient, name: string, key: SlowKey): Promise<string | null> {
  const cached = cachedSlowText(client, name, key)
  if (cached !== undefined) return cached
  const value = await client.getEnsText({ name, key })
  rememberSlowText(client, name, key, value)
  return value
}
