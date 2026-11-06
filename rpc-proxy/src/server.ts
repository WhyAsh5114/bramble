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
      const upstream = await fetch(config.upstreamUrl, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: raw,
        redirect: 'error',
        signal: AbortSignal.timeout(config.upstreamTimeoutMs),
      })
      const text = await upstream.text()
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
