import http from 'node:http'
import type { AgentMcpConfig } from './config'

type RequestStatus = 'pending' | 'approving' | 'approved' | 'denied' | 'revoking' | 'revoked'

interface AccessRequest {
  id: string
  status: RequestStatus
}

const MAX_RESULT_CHARS = 4000
const MAX_BODY_BYTES = 64 << 10

export interface Tools {
  requestPrivateAccess(input: { service: string; reason: string; durationSeconds?: number }): Promise<string>
  callPrivateService(input: { path: string }): Promise<string>
}

export interface ToolDeps {
  fetch?: typeof fetch
  sleep?: (ms: number) => Promise<void>
  now?: () => number
}

class ApprovalApiError extends Error {
  constructor(
    message: string,
    readonly code?: string
  ) {
    super(message)
  }
}

export function createTools(config: AgentMcpConfig, deps: ToolDeps = {}): Tools {
  const doFetch = deps.fetch ?? fetch
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)))
  const now = deps.now ?? Date.now
  const base = config.approvalUrl.replace(/\/+$/, '')
  // Request ids this process created while a decision is still outstanding, keyed by service.
  const outstanding = new Map<string, string>()

  async function api(method: 'GET' | 'POST', path: string, body?: unknown): Promise<AccessRequest> {
    const headers: Record<string, string> = {}
    if (body !== undefined) headers['content-type'] = 'application/json'
    if (method === 'POST') {
      if (!config.approvalToken) throw new ApprovalApiError('APPROVAL_DEMO_TOKEN is not set for this MCP server.')
      headers.authorization = `Bearer ${config.approvalToken}`
    }
    let response: Response
    try {
      response = await doFetch(`${base}${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: AbortSignal.timeout(10_000),
      })
    } catch {
      throw new ApprovalApiError(`cannot reach the approval API at ${base}.`)
    }
    const payload = (await response.json().catch(() => null)) as {
      error?: { code?: string; message?: string }
    } | null
    if (!response.ok) {
      const code = payload?.error?.code ?? `http_${response.status}`
      const detail = payload?.error?.message ? `: ${payload.error.message}` : ''
      throw new ApprovalApiError(`the approval API refused the call (${code})${detail}`, code)
    }
    return payload as unknown as AccessRequest
  }

  function describe(request: AccessRequest, service: string): string | undefined {
    switch (request.status) {
      case 'approved':
        return `granted: a human approved access to ${service}. The gateway applies the grant on its next ENS read, so retry call_private_service if the first call is still denied.`
      case 'denied':
        return `denied: a human rejected the request for ${service}.`
      case 'revoking':
      case 'revoked':
        return `denied: access to ${service} was revoked.`
      default:
        return undefined
    }
  }

  async function awaitDecision(requestId: string, service: string): Promise<string> {
    const deadline = now() + config.decisionTimeoutMs
    for (;;) {
      const request = await api('GET', `/api/requests/${encodeURIComponent(requestId)}`)
      const outcome = describe(request, service)
      if (outcome) {
        outstanding.delete(service)
        return outcome
      }
      if (now() >= deadline) {
        return `pending: no decision after ${Math.round(config.decisionTimeoutMs / 1000)}s. Call request_private_access again to keep waiting on the same request.`
      }
      await sleep(config.pollIntervalMs)
    }
  }

  return {
    async requestPrivateAccess({ service, reason, durationSeconds }) {
      const name = service.trim()
      if (!name) return 'error: service is required.'
      try {
        const existing = outstanding.get(name)
        if (existing) {
          try {
            return await awaitDecision(existing, name)
          } catch (error) {
            // The approval service forgot the request (for example after a restart): start over.
            if (!(error instanceof ApprovalApiError && error.code === 'request_not_found')) throw error
            outstanding.delete(name)
          }
        }
        const created = await api('POST', '/api/requests', {
          agentLabel: config.agentLabel,
          gatewayLabel: config.gatewayLabel,
          service: name,
          note: reason.trim() || undefined,
          requestedDuration: durationSeconds,
        })
        outstanding.set(name, created.id)
        return await awaitDecision(created.id, name)
      } catch (error) {
        if (error instanceof ApprovalApiError) return `error: ${error.message}`
        throw error
      }
    },

    async callPrivateService({ path }) {
      const target = resolveForwardUrl(config.forwardUrl, path)
      if (typeof target === 'string') return target
      return forwardGet(target, config.forwardTimeoutMs)
    },
  }
}

function resolveForwardUrl(forwardUrl: string, path: string): URL | string {
  const segments = path
    .split(/[?#]/)[0]
    .split('/')
    .map((segment) => segment.replace(/%2e/gi, '.'))
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\') || segments.includes('..')) {
    return 'error: path must start with a single "/" and name a path on the private service.'
  }
  const base = new URL(forwardUrl)
  const target = new URL(path, base)
  if (target.origin !== base.origin) {
    return 'error: path must stay on the private service.'
  }
  return target
}

function forwardGet(target: URL, timeoutMs: number): Promise<string> {
  return new Promise((resolve) => {
    const request = http.get(target, { timeout: timeoutMs, agent: false }, (response) => {
      const chunks: Buffer[] = []
      let size = 0
      response.on('data', (chunk: Buffer) => {
        size += chunk.length
        if (size <= MAX_BODY_BYTES) chunks.push(chunk)
      })
      response.on('error', () => resolve(deniedNoGrant()))
      response.on('end', () => {
        const status = response.statusCode ?? 0
        const body = Buffer.concat(chunks).toString('utf8').trim()
        if (status === 403) resolve(deniedNoGrant())
        else if (status >= 200 && status < 300) resolve(`task result: ${truncate(body)}`)
        else resolve(`error: the private service returned HTTP ${status}${body ? `: ${truncate(body)}` : ''}`)
      })
    })
    request.on('timeout', () => {
      request.destroy()
      resolve(`error: no response from the Bramble forward within ${Math.round(timeoutMs / 1000)}s.`)
    })
    request.on('error', (error: NodeJS.ErrnoException) => {
      if (error.code === 'ECONNREFUSED') {
        resolve(`error: the Bramble forward is not listening on ${target.host}. Start brambled with a -forward flag.`)
      } else {
        resolve(deniedNoGrant())
      }
    })
  })
}

// brambled's forward closes the local connection with no reply when the gateway
// answers DENY (gateway/forward.go), and does the same when the tunnel fails.
function deniedNoGrant(): string {
  return 'denied: no grant. The forward closed the connection without a reply, which is how the gateway refuses a call. A broken tunnel looks the same.'
}

function truncate(text: string): string {
  return text.length > MAX_RESULT_CHARS ? `${text.slice(0, MAX_RESULT_CHARS)}… [truncated]` : text
}
