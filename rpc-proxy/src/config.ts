export interface Config {
  /** Dedicated Sepolia endpoint. Read from env only; never logged or returned to clients. */
  upstreamUrl: string
  host: string
  port: number
  /** Number of reverse proxies in front of this process that append to X-Forwarded-For. */
  trustedProxyHops: number
  logCapacity: number
  maxBodyBytes: number
  maxBatchSize: number
  maxLoggedParamsChars: number
  rateLimitPerMinute: number
  upstreamTimeoutMs: number
}

function intEnv(env: NodeJS.ProcessEnv, name: string, fallback: number): number {
  const raw = env[name]
  if (raw === undefined || raw === '') return fallback
  const value = Number(raw)
  if (!Number.isInteger(value) || value < 0) throw new Error(`${name} must be a non-negative integer`)
  return value
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): Config {
  const upstreamUrl = env.UPSTREAM_RPC_URL
  if (!upstreamUrl) throw new Error('UPSTREAM_RPC_URL is required (the dedicated Sepolia endpoint; never commit it)')
  let parsed: URL
  try {
    parsed = new URL(upstreamUrl)
  } catch {
    // Do not echo the value: it can carry an API key.
    throw new Error('UPSTREAM_RPC_URL is not a valid URL')
  }
  if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') throw new Error('UPSTREAM_RPC_URL must be http(s)')
  return {
    upstreamUrl,
    host: env.HOST || '127.0.0.1',
    port: intEnv(env, 'PORT', 8545),
    trustedProxyHops: intEnv(env, 'TRUSTED_PROXY_HOPS', 0),
    logCapacity: intEnv(env, 'LOG_CAPACITY', 2000),
    maxBodyBytes: intEnv(env, 'MAX_BODY_BYTES', 64 * 1024),
    maxBatchSize: intEnv(env, 'MAX_BATCH_SIZE', 20),
    maxLoggedParamsChars: intEnv(env, 'MAX_LOGGED_PARAMS_CHARS', 2048),
    rateLimitPerMinute: intEnv(env, 'RATE_LIMIT_PER_MINUTE', 300),
    upstreamTimeoutMs: intEnv(env, 'UPSTREAM_TIMEOUT_MS', 15_000),
  }
}
