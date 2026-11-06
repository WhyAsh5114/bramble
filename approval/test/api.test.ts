import { expect, spyOn, test } from 'bun:test'
import { randomUUID } from 'node:crypto'
import { createApprovalApi } from '../src/lib/api'

const token = randomUUID()
const input = { agentLabel: 'agent-1', gatewayLabel: 'gateway-1', service: 'web' }

function request(path = '/requests', method = 'GET', body?: unknown, authorized = true) {
  return new Request(`http://localhost/api${path}`, {
    method,
    headers: {
      ...(authorized ? { authorization: `Bearer ${token}` } : {}),
      'content-type': 'application/json',
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  })
}

test('an authenticated suggestion is pending and visible through both GET APIs within two seconds', async () => {
  const api = createApprovalApi({ token: () => token })
  const start = performance.now()
  const response = await api.handle(request('/requests', 'POST', input))
  expect(response.status).toBe(201)
  const created = await response.json()
  expect(created.status).toBe('pending')
  const detail = await api.handle(request(`/requests/${created.id}`))
  expect((await detail.json()).id).toBe(created.id)
  const list = await api.handle(request())
  expect((await list.json()).requests.map((item: { id: string }) => item.id)).toContain(created.id)
  expect(performance.now() - start).toBeLessThan(2000)
})

import { MemoryChain } from './memory-chain'

test('approve, deny and revoke require a human action; duplicate writes fail clearly without a write', async () => {
  const chain = new MemoryChain()
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
  const created = await (await api.handle(request('/requests', 'POST', input))).json()
  expect(chain.writes).toEqual([])
  const approved = await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
  expect(approved.status).toBe(200)
  expect((await approved.json()).status).toBe('approved')
  const duplicate = await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
  expect(duplicate.status).toBe(409)
  expect((await duplicate.json()).error.code).toBe('request_not_pending')
  expect(chain.writes).toEqual(['grant:agent-1'])
  const revoked = await api.handle(request('/grants/agent-1/revoke', 'POST'))
  expect((await revoked.json()).status).toBe('revoked')
  const again = await api.handle(request('/grants/agent-1/revoke', 'POST'))
  expect(again.status).toBe(409)
  expect((await again.json()).error.code).toBe('not_granted')
  expect(chain.writes).toEqual(['grant:agent-1', 'revoke:agent-1'])
  const second = await (await api.handle(request('/requests', 'POST', input))).json()
  const denied = await api.handle(request(`/requests/${second.id}/deny`, 'POST'))
  expect((await denied.json()).status).toBe('denied')
  expect((await api.handle(request(`/requests/${second.id}/approve`, 'POST'))).status).toBe(409)
  expect(chain.writes).toHaveLength(2)
})

test('every write rejects missing or wrong authorization and absent token configuration without mutations', async () => {
  const chain = new MemoryChain()
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
  const created = await (await api.handle(request('/requests', 'POST', input))).json()
  for (const path of [
    '/requests',
    `/requests/${created.id}/approve`,
    `/requests/${created.id}/deny`,
    '/grants/agent-1/revoke',
  ]) {
    expect((await api.handle(request(path, 'POST', input, false))).status).toBe(401)
    const wrong = request(path, 'POST', input)
    wrong.headers.set('authorization', 'Bearer incorrect')
    expect((await api.handle(wrong)).status).toBe(401)
  }
  expect((await (await api.handle(request(`/requests/${created.id}`))).json()).status).toBe('pending')
  expect(chain.writes).toEqual([])
  const unconfigured = createApprovalApi({ token: () => undefined })
  expect((await unconfigured.handle(request('/requests', 'POST', input))).status).toBe(503)
})

test('invalid contracts and malformed JSON fail without accepting a request', async () => {
  const api = createApprovalApi({ token: () => token })
  for (const body of [
    null,
    [],
    {},
    { ...input, agentLabel: 'BAD' },
    { ...input, gatewayLabel: 'a.b' },
    { ...input, service: 'https://internal:3000' },
    { ...input, note: 3 },
    { ...input, note: 'a'.repeat(2001) },
    { ...input, requestedDuration: 0 },
    { ...input, requestedDuration: 86401 },
    { ...input, requestedDuration: 1.2 },
    { ...input, requestedDuration: '60' },
    { ...input, autoApprove: true },
  ]) {
    expect((await api.handle(request('/requests', 'POST', body))).status).toBe(400)
  }
  const malformed = request('/requests', 'POST')
  expect((await api.handle(malformed)).status).toBe(400)
  expect((await (await api.handle(request())).json()).requests).toEqual([])
})

import { ManualClock } from './clock'

test('duration starts at confirmation, counts down, and a server timer revokes without polling', async () => {
  const chain = new MemoryChain()
  const clock = new ManualClock()
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain, clock })
  const created = await (await api.handle(request('/requests', 'POST', { ...input, requestedDuration: 10 }))).json()
  await clock.advance(30000)
  expect(chain.writes).toEqual([])
  const approved = await (await api.handle(request(`/requests/${created.id}/approve`, 'POST'))).json()
  expect(approved.remainingSeconds).toBe(10)
  expect(approved.expiresAt).toBe('2026-11-06T00:00:40.000Z')
  expect(approved.expiryStatus).toBe('scheduled')
  await clock.advance(4000)
  const remaining = await (await api.handle(request(`/requests/${created.id}`))).json()
  expect(remaining.remainingSeconds).toBe(6)
  await clock.advance(6000)
  expect(chain.writes).toEqual(['grant:agent-1', 'revoke:agent-1'])
  const expired = await (await api.handle(request(`/requests/${created.id}`))).json()
  expect(expired.status).toBe('revoked')
  expect(expired.revokeReason).toBe('duration')
  expect(expired.expiryStatus).toBe('revoked')
})

test('failed timed revoke remains granted, reports failure, and retries; manual revoke cancels old timers', async () => {
  const chain = new MemoryChain()
  const clock = new ManualClock()
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain, clock })
  const created = await (await api.handle(request('/requests', 'POST', { ...input, requestedDuration: 2 }))).json()
  await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
  chain.failRevoke = true
  await clock.advance(2000)
  const failed = await (await api.handle(request(`/requests/${created.id}`))).json()
  expect(failed.status).toBe('approved')
  expect(failed.expiryStatus).toBe('failed')
  expect(failed.remainingSeconds).toBe(0)
  expect(chain.grants.has('agent-1')).toBe(true)
  chain.failRevoke = false
  await clock.advance(5000)
  expect(chain.grants.has('agent-1')).toBe(false)
  const second = await (await api.handle(request('/requests', 'POST', { ...input, requestedDuration: 10 }))).json()
  await api.handle(request(`/requests/${second.id}/approve`, 'POST'))
  await api.handle(request('/grants/agent-1/revoke', 'POST'))
  const third = await (await api.handle(request('/requests', 'POST', input))).json()
  await api.handle(request(`/requests/${third.id}/approve`, 'POST'))
  await clock.advance(10000)
  expect(chain.grants.has('agent-1')).toBe(true)
  expect(chain.writes).toHaveLength(5)
})

test('concurrent approve, deny and revoke cannot race a pending signer write', async () => {
  const chain = new MemoryChain()
  let release!: () => void
  const waiting = new Promise<void>((resolve) => {
    release = resolve
  })
  let entered!: () => void
  const writing = new Promise<void>((resolve) => {
    entered = resolve
  })
  chain.beforeGrant = async () => {
    entered()
    await waiting
  }
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
  const created = await (await api.handle(request('/requests', 'POST', input))).json()
  const approving = api.handle(request(`/requests/${created.id}/approve`, 'POST'))
  await writing
  for (const path of [`/requests/${created.id}/approve`, `/requests/${created.id}/deny`, '/grants/agent-1/revoke']) {
    expect((await api.handle(request(path, 'POST'))).status).toBe(409)
  }
  expect((await (await api.handle(request(`/requests/${created.id}`))).json()).status).toBe('approving')
  release()
  expect((await approving).status).toBe(200)
  expect(chain.writes).toEqual(['grant:agent-1'])
})

test('inactive identities and existing ACL grants prevent signing', async () => {
  for (const facts of [null, { pubkeyPublished: false }, { revoked: true }, { expiry: 1 }]) {
    for (const label of ['agent-1', 'gateway-1']) {
      const chain = new MemoryChain()
      if (facts === null) chain.identities.delete(label)
      else chain.identities.set(label, { ...chain.identities.get(label)!, ...facts })
      const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
      const created = await (await api.handle(request('/requests', 'POST', input))).json()
      const result = await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
      expect(result.status).toBe(409)
      expect((await result.json()).error.code).toBe('identity_not_active')
      expect(chain.writes).toEqual([])
    }
  }
  const chain = new MemoryChain()
  chain.grants.add('agent-1')
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
  const created = await (await api.handle(request('/requests', 'POST', input))).json()
  const result = await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
  expect((await result.json()).error.code).toBe('already_granted')
  expect(chain.writes).toEqual([])
})

test('signer failures preserve state, suppress adapter diagnostics and allow a safe retry', async () => {
  const chain = new MemoryChain()
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
  const created = await (await api.handle(request('/requests', 'POST', input))).json()
  chain.failGrant = true
  const failed = await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
  expect(failed.status).toBe(502)
  expect(await failed.text()).not.toContain('sensitive')
  expect((await (await api.handle(request(`/requests/${created.id}`))).json()).status).toBe('pending')
  chain.failGrant = false
  expect((await api.handle(request(`/requests/${created.id}/approve`, 'POST'))).status).toBe(200)
  chain.failRevoke = true
  expect((await api.handle(request('/grants/agent-1/revoke', 'POST'))).status).toBe(502)
  expect((await (await api.handle(request(`/requests/${created.id}`))).json()).status).toBe('approved')
  chain.failRevoke = false
  expect((await api.handle(request('/grants/agent-1/revoke', 'POST'))).status).toBe(200)
})

test('signer failures never log adapter messages, short messages or error names', async () => {
  const chain = new MemoryChain()
  const credential = randomUUID()
  const diagnostic = new Error(`HTTP request failed. Authorization: Bearer ${credential}`)
  diagnostic.name = credential
  Object.assign(diagnostic, { shortMessage: `decrypt failed: ${credential}` })
  chain.grant = async () => {
    throw diagnostic
  }
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
  const created = await (await api.handle(request('/requests', 'POST', input))).json()
  const log = spyOn(console, 'error').mockImplementation(() => {})
  try {
    const failed = await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
    expect(failed.status).toBe(502)
    expect((await failed.text()).includes(credential)).toBe(false)
    const line = String(log.mock.calls[0]?.[0])
    // Compare a boolean so a regression cannot print the diagnostic in a test failure.
    expect(line.includes(credential)).toBe(false)
    expect(line).toBe('approval: signer grant failed; check signer configuration and upstream availability')
  } finally {
    log.mockRestore()
  }
})

test('the real server scheduler revokes after one second without a GET trigger', async () => {
  const chain = new MemoryChain()
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain })
  const created = await (await api.handle(request('/requests', 'POST', { ...input, requestedDuration: 1 }))).json()
  await api.handle(request(`/requests/${created.id}/approve`, 'POST'))
  await new Promise((resolve) => setTimeout(resolve, 1200))
  expect(chain.grants.has('agent-1')).toBe(false)
  const result = await (await api.handle(request(`/requests/${created.id}`))).json()
  expect(result.status).toBe('revoked')
  expect(result.durationMode).toBe('server-timer')
})

test('unconfigured signing fails closed, missing requests return 404, and a fresh instance has no saved state', async () => {
  const api = createApprovalApi({ token: () => token })
  const created = await (await api.handle(request('/requests', 'POST', input))).json()
  expect((await api.handle(request(`/requests/${created.id}/approve`, 'POST'))).status).toBe(503)
  expect((await api.handle(request('/grants/agent-1/revoke', 'POST'))).status).toBe(503)
  expect((await api.handle(request('/requests/missing'))).status).toBe(404)
  expect((await api.handle(request('/requests/missing/deny', 'POST'))).status).toBe(404)
  expect((await api.handle(request())).headers.get('cache-control')).toBe('no-store')
  const fresh = createApprovalApi({ token: () => token })
  expect((await (await fresh.handle(request())).json()).requests).toEqual([])
})

test('an externally revoked grant cannot leave a stale timer that revokes a later approval', async () => {
  const chain = new MemoryChain()
  const clock = new ManualClock()
  const api = createApprovalApi({ token: () => token, signer: chain, resolver: chain, clock })
  const old = await (await api.handle(request('/requests', 'POST', { ...input, requestedDuration: 2 }))).json()
  await api.handle(request(`/requests/${old.id}/approve`, 'POST'))
  chain.grants.delete('agent-1') // Independent admin tooling revokes the first grant.
  const current = await (await api.handle(request('/requests', 'POST', input))).json()
  expect((await api.handle(request(`/requests/${current.id}/approve`, 'POST'))).status).toBe(200)
  await clock.advance(2000)
  expect(chain.grants.has('agent-1')).toBe(true)
  expect(chain.writes).toEqual(['grant:agent-1', 'grant:agent-1'])
  const retired = await (await api.handle(request(`/requests/${old.id}`))).json()
  expect(retired.status).toBe('revoked')
  expect(retired.revokeReason).toBe('external')
})

test('public identity reads expose only resolved facts and fail closed when resolution is unavailable', async () => {
  const chain = new MemoryChain()
  const api = createApprovalApi({ token: () => token, resolver: chain })
  const response = await api.handle(request('/identities/agent-1'))
  expect(response.status).toBe(200)
  expect(await response.json()).toEqual({
    identity: {
      label: 'agent-1',
      pubkeyPublished: true,
      revoked: false,
      expiry: 4102444800,
    },
  })
  expect(await (await api.handle(request('/identities/unknown'))).json()).toEqual({ identity: null })
  const unavailable = createApprovalApi({ token: () => token })
  expect((await unavailable.handle(request('/identities/agent-1'))).status).toBe(503)
  chain.resolveIdentity = async () => {
    throw new Error('private upstream diagnostic')
  }
  const failure = await api.handle(request('/identities/agent-1'))
  expect(failure.status).toBe(502)
  expect(await failure.text()).not.toContain('private upstream')
})
