const MIN_FRAGMENT_CHARS = 8
const GENERIC_ERROR_MESSAGE = 'upstream error'

/** Pieces of the upstream URL that must never reach a client. */
export interface UpstreamSecrets {
  /** Whole URL forms and the full path: matched at any length. */
  whole: string[]
  /** Path segments, query values and credentials: matched when long enough to be a key. */
  fragments: string[]
}

export function secretsFromUrl(upstreamUrl: string): UpstreamSecrets {
  const whole = new Set<string>([upstreamUrl])
  const fragments = new Set<string>()
  const addDecoded = (target: Set<string>, value: string) => {
    if (!value) return
    target.add(value)
    try {
      target.add(decodeURIComponent(value))
    } catch {
      // Not percent-encoded; the raw value is already added.
    }
  }
  try {
    const url = new URL(upstreamUrl)
    whole.add(url.href)
    if (url.pathname.length > 1) addDecoded(whole, url.pathname)
    for (const segment of url.pathname.split('/')) addDecoded(fragments, segment)
    for (const value of url.searchParams.values()) addDecoded(fragments, value)
    addDecoded(fragments, url.username)
    addDecoded(fragments, url.password)
  } catch {
    // An unparseable URL is only matched as a whole.
  }
  return {
    whole: [...whole].filter((s) => s.length > 0),
    fragments: [...fragments].filter((s) => s.length >= MIN_FRAGMENT_CHARS),
  }
}

function leaksSecret(text: string, secrets: UpstreamSecrets): boolean {
  const candidates = [text]
  try {
    candidates.push(decodeURIComponent(text))
  } catch {
    // Not percent-encoded.
  }
  return candidates.some(
    (c) => secrets.whole.some((s) => c.includes(s)) || secrets.fragments.some((s) => c.includes(s))
  )
}

function stringsLeak(value: unknown, secrets: UpstreamSecrets): boolean {
  if (typeof value === 'string') return leaksSecret(value, secrets)
  if (Array.isArray(value)) return value.some((v) => stringsLeak(v, secrets))
  if (typeof value === 'object' && value !== null) {
    return Object.entries(value).some(([k, v]) => leaksSecret(k, secrets) || stringsLeak(v, secrets))
  }
  return false
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function rebuild(entry: unknown, secrets: UpstreamSecrets): Record<string, unknown> | undefined {
  if (!isObject(entry) || entry.jsonrpc !== '2.0') return undefined
  const { id } = entry
  if (!(typeof id === 'string' || typeof id === 'number' || id === null)) return undefined
  if (typeof id === 'string' && leaksSecret(id, secrets)) return undefined
  if ('error' in entry) {
    const error = entry.error
    if (!isObject(error) || !Number.isInteger(error.code)) return undefined
    const out: Record<string, unknown> = { code: error.code, message: GENERIC_ERROR_MESSAGE }
    if (typeof error.data === 'string' && /^0x[0-9a-fA-F]*$/.test(error.data)) out.data = error.data
    return { jsonrpc: '2.0', id, error: out }
  }
  if (!('result' in entry) || stringsLeak(entry.result, secrets)) return undefined
  return { jsonrpc: '2.0', id, result: entry.result }
}

/**
 * Rebuilds an upstream JSON-RPC response from allowed fields only. Error messages are replaced, unknown fields
 * dropped, and a response whose decoded strings contain the upstream URL or its credentials is withheld.
 * Returns undefined when the body is not a valid, safe JSON-RPC response.
 */
export function sanitizeUpstreamBody(text: string, secrets: UpstreamSecrets): string | undefined {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return undefined
  }
  if (Array.isArray(parsed)) {
    if (parsed.length === 0) return undefined
    const rebuilt = parsed.map((entry) => rebuild(entry, secrets))
    return rebuilt.every((r) => r !== undefined) ? JSON.stringify(rebuilt) : undefined
  }
  const single = rebuild(parsed, secrets)
  return single === undefined ? undefined : JSON.stringify(single)
}

export function isJsonMediaType(contentType: string): boolean {
  return /^\s*application\/(?:[\w.-]+\+)?json\s*(?:;|$)/i.test(contentType)
}
