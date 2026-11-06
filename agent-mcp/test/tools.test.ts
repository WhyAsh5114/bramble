import { afterEach, describe, expect, test } from 'bun:test'
import type { AgentMcpConfig } from '../src/config'
import { loadConfig } from '../src/config'
import { createTools } from '../src/tools'
import { startApprovalStub, startForwardStub, type Stub, type StubApproval } from './stubs'

const stubs: Stub[] = []
afterEach(async () => {
  await Promise.all(stubs.splice(0).map((stub) => stub.close()))
})

function track<T extends Stub>(stub: T): T {
  stubs.push(stub)
  return stub
}

function configFor(approval: Stub, forward?: Stub, overrides: Partial<AgentMcpConfig> = {}): AgentMcpConfig {
  return {
    approvalUrl: approval.url,
    approvalToken: 'demo-token',
    agentLabel: 'agent-1',
    gatewayLabel: 'gateway-1',
    forwardUrl: forward?.url ?? 'http://127.0.0.1:1',
    pollIntervalMs: 5,
    decisionTimeoutMs: 2000,
    forwardTimeoutMs: 2000,
    ...overrides,
  }
}

describe('request_private_access', () => {
  test('submits the request and returns granted once a human approves', async () => {
    const approval = track(await startApprovalStub())
    const tools = createTools(configFor(approval), {
      sleep: async () => approval.setStatus('req-1', 'approved'),
    })

    const outcome = await tools.requestPrivateAccess({
      service: 'staging-api',
      reason: 'fetch the health task',
      durationSeconds: 300,
    })

    expect(outcome).toStartWith('granted')
    expect(approval.requests.get('req-1')?.body).toEqual({
      agentLabel: 'agent-1',
      gatewayLabel: 'gateway-1',
      service: 'staging-api',
      note: 'fetch the health task',
      requestedDuration: 300,
    })
  })

  test('returns denied when the human rejects the request', async () => {
    const approval = track(await startApprovalStub())
    const tools = createTools(configFor(approval), {
      sleep: async () => approval.setStatus('req-1', 'denied'),
    })

    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })).toStartWith('denied:')
  })

  test('returns pending on timeout, then resumes the same request instead of duplicating it', async () => {
    const approval = track(await startApprovalStub())
    let clock = 0
    const tools = createTools(configFor(approval, undefined, { decisionTimeoutMs: 100 }), {
      now: () => clock,
      sleep: async (ms) => {
        clock += ms
      },
    })

    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })).toStartWith('pending:')
    approval.setStatus('req-1', 'approved')
    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })).toStartWith('granted')
    expect(approval.posts).toBe(1)
  })

  test('creates a new request after a decided one was reported', async () => {
    const approval = track(await startApprovalStub())
    const tools = createTools(configFor(approval), {
      sleep: async () => approval.setStatus(`req-${approval.posts}`, 'approved'),
    })

    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })).toStartWith('granted')
    approval.setStatus('req-1', 'revoked')
    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'again' })).toStartWith('granted')
    expect(approval.posts).toBe(2)
  })

  test('reports an approval API refusal without throwing', async () => {
    const approval = track(await startApprovalStub())
    const tools = createTools(configFor(approval, undefined, { approvalToken: 'wrong' }))

    const outcome = await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })

    expect(outcome).toBe(
      'error: the approval API refused the call (unauthorized): A valid demo bearer token is required.'
    )
  })

  test('starts a fresh request when the approval service forgot the outstanding one', async () => {
    const approval = track(await startApprovalStub())
    let clock = 0
    const tools = createTools(configFor(approval, undefined, { decisionTimeoutMs: 100 }), {
      now: () => clock,
      sleep: async (ms) => {
        clock += ms
      },
    })

    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })).toStartWith('pending:')
    approval.requests.clear()
    approval.setStatus('req-1', 'approved')
    clock = 0
    const outcome = await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })

    expect(outcome).toStartWith('pending:')
    expect(approval.posts).toBe(2)
    expect(approval.requests.get('req-1')?.status).toBe('pending')
  })

  test('reports a missing token before any network call', async () => {
    const approval = track(await startApprovalStub())
    const tools = createTools(configFor(approval, undefined, { approvalToken: undefined }))

    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })).toContain('APPROVAL_DEMO_TOKEN')
    expect(approval.posts).toBe(0)
  })

  test('reports an unreachable approval API', async () => {
    const approval = await startApprovalStub()
    await approval.close()
    const tools = createTools(configFor(approval))

    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'x' })).toStartWith(
      'error: cannot reach the approval API'
    )
  })

  test('rejects an empty service name', async () => {
    const approval = track(await startApprovalStub())
    const tools = createTools(configFor(approval))

    expect(await tools.requestPrivateAccess({ service: '  ', reason: 'x' })).toStartWith('error:')
    expect(approval.posts).toBe(0)
  })
})

describe('call_private_service', () => {
  test('returns the task result when the forward allows the call', async () => {
    const approval = track(await startApprovalStub())
    const forward = track(await startForwardStub('allow'))
    const tools = createTools(configFor(approval, forward))

    const outcome = await tools.callPrivateService({ path: '/task' })

    expect(outcome).toStartWith('task result: ')
    expect(outcome).toContain('report-private-service-health')
    expect(forward.paths).toEqual(['/task'])
  })

  test('returns denied when the forward closes the connection without a reply', async () => {
    const approval = track(await startApprovalStub())
    const forward = track(await startForwardStub('deny-close'))
    const tools = createTools(configFor(approval, forward))

    expect(await tools.callPrivateService({ path: '/task' })).toStartWith('denied: no grant')
  })

  test('returns denied on an HTTP 403 from the forward', async () => {
    const approval = track(await startApprovalStub())
    const forward = track(await startForwardStub('deny-403'))
    const tools = createTools(configFor(approval, forward))

    expect(await tools.callPrivateService({ path: '/task' })).toStartWith('denied: no grant')
  })

  test('allows ".." inside a query string', async () => {
    const approval = track(await startApprovalStub())
    const forward = track(await startForwardStub('allow'))
    const tools = createTools(configFor(approval, forward))

    expect(await tools.callPrivateService({ path: '/task?x=../y' })).toStartWith('task result: ')
    expect(forward.paths).toEqual(['/task?x=../y'])
  })

  test('says the forward is down when nothing listens', async () => {
    const approval = track(await startApprovalStub())
    const forward = await startForwardStub('allow')
    await forward.close()
    const tools = createTools(configFor(approval, forward))

    expect(await tools.callPrivateService({ path: '/task' })).toStartWith('error: the Bramble forward is not listening')
  })

  test.each([
    'task',
    '//evil.example/task',
    'http://evil.example/task',
    '/../etc/passwd',
    '/a\\b',
    '/a/.%2e/b',
    '/a/%2e./b',
    '/a/%2E%2e/b',
  ])('refuses the path %p without calling the forward', async (path) => {
    const approval = track(await startApprovalStub())
    const forward = track(await startForwardStub('allow'))
    const tools = createTools(configFor(approval, forward))

    expect(await tools.callPrivateService({ path })).toStartWith('error:')
    expect(forward.paths).toEqual([])
  })
})

describe('full sequence: deny, request, approve, succeed, revoke, deny', () => {
  test('follows the demo loop with no restart', async () => {
    const approval: StubApproval = track(await startApprovalStub())
    const forward = track(await startForwardStub('deny-close'))
    const tools = createTools(configFor(approval, forward), {
      sleep: async () => {
        approval.setStatus('req-1', 'approved')
        forward.mode = 'allow'
      },
    })

    expect(await tools.callPrivateService({ path: '/task' })).toStartWith('denied: no grant')
    expect(await tools.requestPrivateAccess({ service: 'staging-api', reason: 'health task' })).toStartWith('granted')
    expect(await tools.callPrivateService({ path: '/task' })).toStartWith('task result: ')
    forward.mode = 'deny-close'
    expect(await tools.callPrivateService({ path: '/task' })).toStartWith('denied: no grant')
  })
})

describe('loadConfig', () => {
  const base = { BRAMBLE_AGENT_LABEL: 'agent-1', BRAMBLE_GATEWAY_LABEL: 'gateway-1' }

  test('applies defaults', () => {
    const config = loadConfig(base)
    expect(config.approvalUrl).toBe('http://127.0.0.1:3001')
    expect(config.forwardUrl).toBe('http://127.0.0.1:8080')
    expect(config.approvalToken).toBeUndefined()
  })

  test('requires the agent and gateway labels', () => {
    expect(() => loadConfig({ BRAMBLE_GATEWAY_LABEL: 'g' })).toThrow('BRAMBLE_AGENT_LABEL')
    expect(() => loadConfig({ BRAMBLE_AGENT_LABEL: 'a' })).toThrow('BRAMBLE_GATEWAY_LABEL')
  })

  test('refuses a non-loopback forward', () => {
    expect(() => loadConfig({ ...base, BRAMBLE_FORWARD_URL: 'http://example.com:8080' })).toThrow('loopback')
  })

  test.each(['https://127.0.0.1:8080', 'ftp://127.0.0.1'])('refuses the forward URL %p that is not http', (url) => {
    expect(() => loadConfig({ ...base, BRAMBLE_FORWARD_URL: url })).toThrow('http://')
  })

  test('rejects a non-positive timing value', () => {
    expect(() => loadConfig({ ...base, BRAMBLE_POLL_INTERVAL_MS: '0' })).toThrow('BRAMBLE_POLL_INTERVAL_MS')
  })
})
