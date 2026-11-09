import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import type { Config } from './config.ts'
import { MemoryLog, truncate } from './log.ts'
import { RateLimiter } from './rate-limit.ts'
import { isJsonMediaType, sanitizeUpstreamBody, secretsFromUrl } from './sanitize.ts'

const ALLOWED_METHOD_PREFIXES = ['eth_', 'net_', 'web3_']

type JsonRpcId = string | number | null

class BodyTooLargeError extends Error {}

function normalizeIp(ip: string | undefined): string {
  if (!ip) return 'unknown'
  return ip.startsWith('::ffff:') ? ip.slice('::ffff:'.length) : ip
}

/** Source IP: the socket address, or the Nth-from-last X-Forwarded-For entry behind trusted proxies. */
export function sourceIp(req: IncomingMessage, trustedProxyHops: number): string {
  if (trustedProxyHops > 0) {
    const header = req.headers['x-forwarded-for']
    const parts = (Array.isArray(header) ? header.join(',') : (header ?? ''))
      .split(',')
      .map((p) => p.trim())
      .filter(Boolean)
    const pick = parts[parts.length - trustedProxyHops]
    if (pick) return normalizeIp(pick)
  }
  return normalizeIp(req.socket.remoteAddress)
}

function readBody(req: IncomingMessage, maxBytes: number): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = []
    let size = 0
    let tooLarge = false
    req.on('data', (chunk: Buffer) => {
      if (tooLarge) return
      size += chunk.length
      if (size > maxBytes) {
        tooLarge = true
        chunks.length = 0
        reject(new BodyTooLargeError())
        return
      }
      chunks.push(chunk)
    })
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf-8')))
    req.on('error', reject)
  })
}

function sendJson(res: ServerResponse, status: number, body: unknown, extraHeaders: Record<string, string> = {}): void {
  const text = JSON.stringify(body)
  res.writeHead(status, {
    'content-type': 'application/json',
    'content-length': Buffer.byteLength(text),
    ...extraHeaders,
  })
  res.end(text)
}

function rpcError(id: JsonRpcId, code: number, message: string) {
  return { jsonrpc: '2.0', id, error: { code, message } }
}

/** Identical retries of a single eth_getLogs the upstream rejects as invalid params. */
const GET_LOGS_RETRIES = 2

// isInvalidParams reports whether an upstream body is a JSON-RPC error with code -32602.
function isInvalidParams(text: string): boolean {
  try {
    const body: unknown = JSON.parse(text)
    return isObject(body) && isObject(body.error) && body.error.code === -32602
  } catch {
    return false
  }
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export interface ProxyServer {
  server: Server
  log: MemoryLog
}

export function createProxyServer(config: Config): ProxyServer {
  const log = new MemoryLog(config.logCapacity)
  const limiter = new RateLimiter(config.rateLimitPerMinute)
  const upstreams = [config.upstreamUrl, ...(process.env.UPSTREAM_FALLBACK_URLS ?? '').split(',').filter(Boolean)]
  let upstreamCursor = 0
  const secrets = secretsFromUrl(config.upstreamUrl)

  function record(ip: string, call: unknown): string {
    const time = new Date().toISOString()
    if (!isObject(call) || typeof call.method !== 'string') {
      log.add({ time, sourceIp: ip, method: '(invalid)', params: '' })
      return '(invalid)'
    }
    const params = call.params === undefined ? '' : (JSON.stringify(call.params) ?? '')
    log.add({ time, sourceIp: ip, method: call.method, params: truncate(params, config.maxLoggedParamsChars) })
    return call.method
  }

  async function handleRpc(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const ip = sourceIp(req, config.trustedProxyHops)
    if (!limiter.allow(ip)) {
      sendJson(res, 429, rpcError(null, -32005, 'rate limit exceeded'), { 'retry-after': '60' })
      return
    }

    let raw: string
    try {
      raw = await readBody(req, config.maxBodyBytes)
    } catch (err) {
      if (err instanceof BodyTooLargeError) {
        sendJson(res, 413, rpcError(null, -32600, 'request body too large'), { connection: 'close' })
      } else if (!res.headersSent) {
        sendJson(res, 400, rpcError(null, -32700, 'could not read request body'))
      }
      return
    }

    let parsed: unknown
    try {
      parsed = JSON.parse(raw)
    } catch {
      record(ip, undefined)
      sendJson(res, 400, rpcError(null, -32700, 'parse error'))
      return
    }

    const calls = Array.isArray(parsed) ? parsed : [parsed]
    if (calls.length === 0 || calls.length > config.maxBatchSize) {
      record(ip, undefined)
      sendJson(res, 400, rpcError(null, -32600, `batch must contain 1 to ${config.maxBatchSize} calls`))
      return
    }

    const methods = calls.map((call) => record(ip, call))
    const blocked = methods.find((m) => !ALLOWED_METHOD_PREFIXES.some((p) => m.startsWith(p)))
    if (blocked !== undefined) {
      sendJson(res, 403, rpcError(null, -32601, 'method not allowed by this proxy'))
      return
    }

    try {
      // Rotates across upstreams and moves on when one is rate limiting or failing.
      const callUpstream = async (body: string) => {
        const start = upstreamCursor++
        let last: Response | undefined
        let failure: unknown
        for (let i = 0; i < upstreams.length; i++) {
          try {
            const response = await fetch(upstreams[(start + i) % upstreams.length], {
              method: 'POST',
              headers: { 'content-type': 'application/json' },
              body,
              redirect: 'error',
              signal: AbortSignal.timeout(config.upstreamTimeoutMs),
            })
            const text = await response.text()
            last = new Response(text, {
              status: response.status,
              headers: { 'content-type': response.headers.get('content-type') ?? '' },
            })
            const limited =
              response.status === 429 ||
              response.status >= 500 ||
              /exceeds defined limit|rate limit|too many requests/i.test(text)
            if (!limited) return last
          } catch (err) {
            failure = err
          }
        }
        if (last) return last
        throw failure
      }
      let upstream = await callUpstream(raw)
      let text = await upstream.text()
      // The upstream is a load-balanced pool. A getLogs whose toBlock is the head that eth_blockNumber just
      // returned can land on a backend that has not seen that block yet, and that backend answers -32602.
      // Retry a single call with the identical body, so the caller's block bounds are never changed here: a
      // pinned or historical window must return exactly the range it asked for. Callers that can safely widen
      // an unpinned head bound to "latest" do that themselves (sidecar/src/ens/scan.ts). The retries are not
      // logged: the log records what clients sent, not what the proxy did.
      if (config.upstreamRetryDelayMs > 0 && calls.length === 1 && methods[0] === 'eth_getLogs') {
        for (let retry = 0; retry < GET_LOGS_RETRIES && isInvalidParams(text); retry++) {
          await new Promise((resolve) => setTimeout(resolve, config.upstreamRetryDelayMs))
          upstream = await callUpstream(raw)
          text = await upstream.text()
        }
      }
      // The response is rebuilt from allowed JSON-RPC fields: provider errors can echo the request URL in many forms.
      const safe = isJsonMediaType(upstream.headers.get('content-type') ?? '')
        ? sanitizeUpstreamBody(text, secrets)
        : undefined
      if (safe === undefined) {
        sendJson(res, 502, rpcError(null, -32603, 'upstream returned an invalid or unsafe response'))
        return
      }
      const body = Buffer.from(safe, 'utf-8')
      res.writeHead(upstream.status, { 'content-type': 'application/json', 'content-length': body.length })
      res.end(body)
    } catch {
      // Never echo the error: fetch failures can include the upstream URL.
      sendJson(res, 502, rpcError(null, -32603, 'upstream unavailable'))
    }
  }

  function handleLog(url: URL, res: ServerResponse): void {
    const since = Number(url.searchParams.get('since') ?? 0)
    const limit = Number(url.searchParams.get('limit') ?? config.logCapacity)
    const view = log.view(
      Number.isFinite(since) && since >= 0 ? since : 0,
      Number.isInteger(limit) && limit > 0 ? limit : config.logCapacity
    )
    sendJson(res, 200, view, { 'access-control-allow-origin': '*', 'cache-control': 'no-store' })
  }

  const server = createServer((req, res) => {
    let url: URL
    try {
      url = new URL(req.url ?? '/', 'http://localhost')
    } catch {
      sendJson(res, 400, { error: 'bad request target' })
      return
    }
    const path = url.pathname

    if ((path === '/' || path === '/rpc') && req.method === 'POST') {
      handleRpc(req, res).catch(() => {
        if (!res.headersSent) sendJson(res, 500, rpcError(null, -32603, 'internal error'))
      })
    } else if (path === '/log' && req.method === 'GET') {
      handleLog(url, res)
    } else if (path === '/log' && req.method === 'OPTIONS') {
      res.writeHead(204, {
        'access-control-allow-origin': '*',
        'access-control-allow-methods': 'GET, OPTIONS',
        'access-control-max-age': '600',
      })
      res.end()
    } else if (path === '/healthz' && req.method === 'GET') {
      sendJson(res, 200, { ok: true })
    } else {
      sendJson(res, 404, { error: 'not found' })
    }
  })

  return { server, log }
}
