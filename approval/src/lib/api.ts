import { timingSafeEqual } from 'node:crypto'
import { ApiError, type GrantSigner, type IdentityResolver, type Clock } from './contracts'
import { ApprovalService } from './service'
import { readInput, label } from './validation'

interface ApiOptions {
  signer?: GrantSigner
  resolver?: IdentityResolver
  clock?: Clock
  token: () => string | undefined
}

export function createApprovalApi(options: ApiOptions) {
  const service = new ApprovalService(options.signer, options.resolver, options.clock)
  const json = (value: unknown, status = 200) =>
    Response.json(value, { status, headers: { 'cache-control': 'no-store' } })

  return {
    async handle(request: Request): Promise<Response> {
      try {
        if (request.method === 'POST') {
          const token = options.token()
          if (!token) throw new ApiError(503, 'token_not_configured', 'APPROVAL_DEMO_TOKEN is required for writes.')
          const provided = Buffer.from(request.headers.get('authorization') ?? '')
          const expected = Buffer.from(`Bearer ${token}`)
          if (provided.length !== expected.length || !timingSafeEqual(provided, expected)) {
            throw new ApiError(401, 'unauthorized', 'A valid demo bearer token is required.')
          }
        }
        const path = new URL(request.url).pathname.replace(/\/$/, '')
        if (path === '/api/requests' && request.method === 'POST') {
          return json(service.create(await readInput(request)), 201)
        }
        if (path === '/api/requests' && request.method === 'GET') return json({ requests: service.list() })
        const decision = /^\/api\/requests\/([^/]+)\/(approve|deny)$/.exec(path)
        if (decision && request.method === 'POST') {
          return json(decision[2] === 'approve' ? await service.approve(decision[1]) : service.deny(decision[1]))
        }
        const revoke = /^\/api\/grants\/([^/]+)\/revoke$/.exec(path)
        if (revoke && request.method === 'POST') return json(await service.revoke(label(revoke[1], 'agentLabel')))
        const detail = /^\/api\/requests\/([^/]+)$/.exec(path)
        if (detail && request.method === 'GET') return json(service.get(detail[1]))
        throw new ApiError(404, 'route_not_found', 'Route not found.')
      } catch (error) {
        if (error instanceof ApiError)
          return json({ error: { code: error.code, message: error.message } }, error.status)
        return json({ error: { code: 'internal_error', message: 'The operation failed.' } }, 500)
      }
    },
  }
}
