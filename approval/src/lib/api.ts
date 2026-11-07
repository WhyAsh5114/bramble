import { timingSafeEqual } from 'node:crypto'
import {
  ApiError,
  type GrantSigner,
  type IdentityResolver,
  type Clock,
  type PaymentInput,
  type JoinChain,
} from './contracts'
import { JoinService } from './joins'
import { ApprovalService, systemClock } from './service'
import { AuditRecorder, type AuditTrail } from './audit'
import { parsePayment, readInput, readPayment, readJoin, label } from './validation'
import { createAuditVerificationApi, type AuditSource } from './audit-verification'
import { loadActivity, type PaymentsSource, type StatusSource } from './activity'
import { addNetworkFacts, createNetworkSource, type NetworkSource } from './network'

interface ApiOptions {
  network?: NetworkSource
  auditSource?: AuditSource
  joinChain?: JoinChain
  signer?: GrantSigner
  resolver?: IdentityResolver
  clock?: Clock
  /** Posts salted commitments to HCS. Without it, audit feed rows report `audit_not_configured`. */
  auditTrail?: AuditTrail
  /** Parent ENS name of agent labels, for example `acme.eth`. */
  ensParent?: string
  /**
   * Settled payments this process observes itself, for example from the mirror node. Each is
   * polled and recorded once per `txRef`; nothing here is believed on a caller's word.
   */
  settlements?: () => Promise<{ settled: PaymentInput[]; unsupported?: string[] }>
  /** Delay between settlement polls. Defaults to 30 seconds. */
  settlementPollMs?: number
  token: () => string | undefined
  /** Read-only feed sources. An omitted source reports `not_configured`. */
  activity?: { status?: StatusSource; payments?: PaymentsSource }
}

export function createApprovalApi(options: ApiOptions) {
  const network =
    options.network ??
    createNetworkSource({
      baseUrl: () => undefined,
      dataRelays: () => process.env.BRAMBLE_DATA_RELAYS,
      rendezvousUrl: () => process.env.BRAMBLE_RENDEZVOUS_PAYMENT_URL,
    })
  const clock = options.clock ?? systemClock
  const audit = new AuditRecorder(options.auditTrail, clock)
  const verifyAudit = createAuditVerificationApi({ token: options.token, source: () => options.auditSource ?? audit })
  const service = new ApprovalService(options.signer, options.resolver, clock, audit, options.ensParent)
  const joins = new JoinService(options.joinChain, options.ensParent, clock)
  const json = (value: unknown, status = 200) =>
    Response.json(value, { status, headers: { 'cache-control': 'no-store' } })

  // 202 while a submitted transaction is unconfirmed: the write may still land.
  const jsonWrite = (value: { status?: string } | undefined) =>
    json(value, value?.status === 'approving' || value?.status === 'revoking' ? 202 : 200)

  // A source that is down or returns a malformed row skips that poll; the next one retries.
  const observeSettlements = async () => {
    if (!options.settlements) return
    try {
      const { settled, unsupported = [] } = await options.settlements()
      for (const payment of settled) {
        try {
          service.recordPayment(parsePayment({ ...payment }))
        } catch {
          // A malformed row is not recorded.
        }
      }
      for (const txRef of unsupported) service.recordUnsupportedPayment(txRef)
    } catch {
      // The source is unavailable.
    }
  }
  if (options.settlements) {
    const pollMs = options.settlementPollMs ?? 30_000
    const tick = async () => {
      await observeSettlements()
      clock.schedule(tick, pollMs)
    }
    clock.schedule(tick, 0)
  }

  return {
    audit,
    observeSettlements,
    async handle(request: Request): Promise<Response> {
      try {
        const path = new URL(request.url).pathname.replace(/\/$/, '')
        const openJoin = path === '/api/joins' && request.method === 'POST'
        const openAccess = path === '/api/joins/access' && request.method === 'POST'
        if (
          (!openJoin && !openAccess && request.method === 'POST') ||
          (request.method === 'GET' && (path === '/api/joins' || path === '/api/members' || path === '/api/network'))
        ) {
          const token = options.token()
          if (!token) throw new ApiError(503, 'token_not_configured', 'APPROVAL_DEMO_TOKEN is required for writes.')
          const provided = Buffer.from(request.headers.get('authorization') ?? '')
          const expected = Buffer.from(`Bearer ${token}`)
          if (provided.length !== expected.length || !timingSafeEqual(provided, expected)) {
            throw new ApiError(401, 'unauthorized', 'A valid demo bearer token is required.')
          }
        }
        if (path === '/api/audit/verification' && request.method === 'GET') return verifyAudit(request)
        if (path === '/api/network' && request.method === 'GET') {
          const view = await network.read()
          let members: Awaited<ReturnType<JoinService['members']>> = []
          let membershipState: 'ok' | 'not_configured' | 'unavailable' = options.joinChain ? 'ok' : 'not_configured'
          if (options.joinChain) {
            try {
              members = await joins.members()
            } catch {
              membershipState = 'unavailable'
            }
          }
          const knownDevices = [
            ...members,
            ...joins.list().filter((join) => !members.some((member) => member.label === join.label)),
          ]
          return json({ ...addNetworkFacts(view, service.list(), knownDevices, options.ensParent), membershipState })
        }
        if (path === '/api/network/ping' && request.method === 'POST') {
          const text = await request.text()
          if (text.length > 1024) throw new ApiError(400, 'invalid_node', 'A known node identifier is required.')
          let body: unknown
          try {
            body = JSON.parse(text)
          } catch {
            throw new ApiError(400, 'invalid_node', 'A known node identifier is required.')
          }
          if (
            !body ||
            typeof body !== 'object' ||
            !('id' in body) ||
            typeof body.id !== 'string' ||
            !/^(gateway|(?:peer|relay|member):[a-zA-Z0-9][a-zA-Z0-9.-]{0,252})$/.test(body.id)
          )
            throw new ApiError(400, 'invalid_node', 'A known node identifier is required.')
          return json(await network.ping(body.id))
        }
        if (openAccess) {
          // Open like a join: a member device asks for a service, and it only
          // creates a pending card that the operator must still approve.
          joins.limit()
          const input = await readInput(request)
          await joins.requireMember(input.agentLabel)
          const open = service
            .list()
            .find(
              (item) =>
                item.agentLabel === input.agentLabel &&
                item.service === input.service &&
                item.status !== 'denied' &&
                item.status !== 'revoked'
            )
          return json(open ?? service.create(input), open ? 200 : 201)
        }
        if (openJoin) {
          joins.limit()
          return json(joins.create(await readJoin(request)), 201)
        }
        if (path === '/api/joins' && request.method === 'GET') return json({ joins: joins.list() })
        if (path === '/api/members' && request.method === 'GET') return json({ members: await joins.members() })
        const joinDecision = /^\/api\/joins\/([^/]+)\/(approve|deny|revoke)$/.exec(path)
        if (joinDecision && request.method === 'POST') {
          const id = decodeURIComponent(joinDecision[1])
          const action = joinDecision[2]
          return json(
            action === 'deny' ? joins.deny(id) : action === 'approve' ? await joins.approve(id) : await joins.revoke(id)
          )
        }
        const joinDetail = /^\/api\/joins\/([^/]+)$/.exec(path)
        if (joinDetail && request.method === 'GET') return json(joins.get(decodeURIComponent(joinDetail[1])))
        if (path === '/api/requests' && request.method === 'POST') {
          return json(service.create(await readInput(request)), 201)
        }
        if (path === '/api/requests' && request.method === 'GET') return json({ requests: service.list() })
        if (path === '/api/activity' && request.method === 'GET') {
          return json(
            await loadActivity({ requests: service.list(), ...options.activity, ensParent: options.ensParent })
          )
        }
        if (path === '/api/audit' && request.method === 'GET') return json({ entries: service.auditFeed() })
        if (path === '/api/payments/settled' && request.method === 'POST') {
          return json(service.recordPayment(await readPayment(request)), 202)
        }
        const identity = /^\/api\/identities\/([^/]+)$/.exec(path)
        if (identity && request.method === 'GET') {
          const name = label(identity[1], 'agentLabel')
          if (!options.resolver) {
            throw new ApiError(503, 'resolver_not_configured', 'Identity resolution is not configured.')
          }
          try {
            const facts = await options.resolver.resolveIdentity(name)
            return json({
              identity: facts
                ? {
                    label: facts.label,
                    pubkeyPublished: facts.pubkeyPublished,
                    revoked: facts.revoked,
                    expiry: facts.expiry,
                  }
                : null,
            })
          } catch {
            throw new ApiError(502, 'identity_unavailable', 'ENS identity facts are temporarily unavailable.')
          }
        }
        const decision = /^\/api\/requests\/([^/]+)\/(approve|deny)$/.exec(path)
        if (decision && request.method === 'POST') {
          if (decision[2] === 'deny') return json(service.deny(decision[1]))
          return jsonWrite(await service.approve(decision[1]))
        }
        const revoke = /^\/api\/grants\/([^/]+)\/revoke$/.exec(path)
        if (revoke && request.method === 'POST') return jsonWrite(await service.revoke(label(revoke[1], 'agentLabel')))
        const detail = /^\/api\/requests\/([^/]+)$/.exec(path)
        if (detail && request.method === 'GET') return json(service.get(detail[1]))
        throw new ApiError(404, 'route_not_found', 'Route not found.')
      } catch (error) {
        if (error instanceof ApiError)
          return Response.json(
            { error: { code: error.code, message: error.message } },
            { status: error.status, headers: { 'cache-control': 'no-store', ...error.headers } }
          )
        return json({ error: { code: 'internal_error', message: 'The operation failed.' } }, 500)
      }
    },
  }
}
