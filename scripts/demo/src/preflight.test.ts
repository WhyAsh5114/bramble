import { describe, expect, it } from 'bun:test'
import {
  buildChecks,
  exitCode,
  formatReport,
  formatUsdc,
  missingEnvNames,
  runPreflight,
  type CheckResult,
  type CheckUnit,
  type Env,
  type HttpResponse,
  type NodeProbe,
  type PreflightContext,
} from './preflight'
import { NODE_ROLES } from './topology'

const USDC = '0.0.429274'
const SECRET = 'sk-live-0123456789abcdef'

const GOOD_NODE_ENV: Env = {
  BRAMBLE_NODE_LABEL: 'gateway',
  BRAMBLE_TAILNET_NAME: 'mesh.eth',
  BRAMBLE_TAILNET_REGISTRY: '0x1',
  BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK: '1',
  BRAMBLE_RELAY_REGISTRY: '0x2',
  BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK: '1',
  BRAMBLE_PRIVATE_KEY: SECRET,
  HEDERA_CLIENT_ACCOUNT_ID: '0.0.1001',
  HEDERA_CLIENT_PRIVATE_KEY: SECRET,
  BRAMBLE_SERVICE_NAME: 'web',
  BRAMBLE_GATEWAY_LABEL: 'gateway',
}

type Routes = Record<string, HttpResponse | (() => HttpResponse) | Error>

function probe(routes: Routes, describe = 'node'): NodeProbe {
  return {
    describe,
    async getJson(port, path) {
      const hit = routes[`${port}${path}`]
      if (hit === undefined) return { status: 404, body: {} }
      if (hit instanceof Error) throw hit
      return typeof hit === 'function' ? hit() : hit
    },
  }
}

const HEALTHY: Routes = {
  '7890/health': { status: 200, body: { ok: true, tailnetName: 'mesh.eth' } },
  '7890/tor/status': { status: 200, body: { state: 'ready' } },
  '7899/endpoint': {
    status: 200,
    body: { state: 'discovered', endpoint: '198.51.100.4:54321', server: 'stun.example.org:3478' },
  },
  '7890/devices': { status: 200, body: { labels: ['gateway', 'worker', 'agent'] } },
  '7890/relays': {
    status: 200,
    body: {
      rendezvous: [{ label: 'relay1', address: 'relay.example.org:3478', sidecarUrl: 'https://pay.example.org' }],
      dataRelays: [],
    },
  },
}

function context(over: Partial<PreflightContext> = {}): PreflightContext {
  return {
    env: {
      BRAMBLE_TAILNET_NAME: 'mesh.eth',
      DEMO_AGENT_LABEL: 'agent',
      HEDERA_CLIENT_ACCOUNT_ID: '0.0.1001',
      ...GOOD_NODE_ENV,
    },
    localRoles: ['gateway'],
    roleEnv: {},
    nodes: { gateway: probe(HEALTHY, 'this machine'), worker: probe(HEALTHY, 'worker') },
    async http(url) {
      if (url.includes('/api/v1/accounts/')) {
        return { status: 200, body: { tokens: [{ token_id: USDC, balance: 2_000_000 }] } }
      }
      return { status: 200, body: {} }
    },
    async tcp() {},
    granterRingPresent: true,
    ...over,
  }
}

async function run(ctx: PreflightContext, waitMs = 0): Promise<CheckResult[]> {
  return runPreflight(buildChecks(ctx), { waitMs, intervalMs: 10, sleep: async () => {} })
}

function byId(results: CheckResult[], id: string): CheckResult {
  const found = results.find((r) => r.id === id)
  if (!found) throw new Error(`no result ${id}; have ${results.map((r) => r.id).join(', ')}`)
  return found
}

describe('env names', () => {
  it('reports missing names and never a value', async () => {
    const env = { ...GOOD_NODE_ENV }
    delete env.HEDERA_CLIENT_PRIVATE_KEY
    const results = await run(context({ env }))
    const r = byId(results, 'env:gateway')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('HEDERA_CLIENT_PRIVATE_KEY')
    expect(JSON.stringify(results)).not.toContain(SECRET)
  })

  it('treats an empty value as missing', () => {
    const ctx = context({ env: { ...GOOD_NODE_ENV, BRAMBLE_PRIVATE_KEY: '  ' } })
    expect(missingEnvNames(ctx, 'gateway')).toEqual(['BRAMBLE_PRIVATE_KEY'])
  })

  it('checks a role against its own env file when one is given', () => {
    const ctx = context({ env: {}, roleEnv: { gateway: GOOD_NODE_ENV } })
    expect(missingEnvNames(ctx, 'gateway')).toEqual([])
  })

  it('admin needs the granter key (env or Key Ring) and a HEDERA_SERVER_ name', () => {
    const adminEnv: Env = {
      BRAMBLE_TAILNET_NAME: 'mesh.eth',
      BRAMBLE_TAILNET_REGISTRY: '0x1',
      BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK: '1',
      BRAMBLE_RELAY_REGISTRY: '0x2',
      BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK: '1',
      SEPOLIA_PRIVATE_KEY: SECRET,
      APPROVAL_DEMO_TOKEN: SECRET,
    }
    const none = context({ env: adminEnv, localRoles: ['admin'], granterRingPresent: false })
    expect(missingEnvNames(none, 'admin')).toEqual(['BRAMBLE_GRANTER_PRIVATE_KEY', 'HEDERA_SERVER_*'])

    const ok = context({
      env: { ...adminEnv, HEDERA_SERVER_ACCOUNT_ID: '0.0.2', BRAMBLE_GRANTER_PRIVATE_KEY: SECRET },
      localRoles: ['admin'],
      granterRingPresent: false,
    })
    expect(missingEnvNames(ok, 'admin')).toEqual([])
  })

  it('SEPOLIA_PRIVATE_KEY missing on admin is reported by name only', async () => {
    const env: Env = { BRAMBLE_GRANTER_PRIVATE_KEY: SECRET, HEDERA_SERVER_KEY: SECRET }
    const results = await run(context({ env, localRoles: ['admin'] }))
    const r = byId(results, 'env:admin')
    expect(r.detail).toContain('SEPOLIA_PRIVATE_KEY')
    expect(JSON.stringify(results)).not.toContain(SECRET)
  })
})

describe('sepolia rpc', () => {
  it('passes with a warning when SEPOLIA_RPC_URL is unset', async () => {
    const r = byId(await run(context()), 'sepolia-rpc')
    expect(r.status).toBe('warn')
    expect(r.detail).toContain('stale')
  })

  it('passes and does not print the value when it is set', async () => {
    const url = 'https://eth-sepolia.example.org/v2/abcdef0123456789'
    const results = await run(context({ env: { ...context().env, SEPOLIA_RPC_URL: url } }))
    const r = byId(results, 'sepolia-rpc')
    expect(r.status).toBe('pass')
    expect(JSON.stringify(results)).not.toContain('abcdef0123456789')
  })

  it('a warning alone leaves the exit code at zero', async () => {
    const results = await run(context())
    expect(byId(results, 'sepolia-rpc').status).toBe('warn')
    expect(exitCode(results)).toBe(0)
  })
})

describe('node checks', () => {
  it('everything healthy passes', async () => {
    const results = await run(context())
    expect(results.filter((r) => r.status === 'fail')).toEqual([])
    expect(byId(results, 'stun:gateway').detail).toContain('198.51.100.4:54321')
  })

  it('a node that cannot be reached is a failure, not a skip', async () => {
    const results = await run(context({ nodes: { gateway: probe(HEALTHY) } }))
    const r = byId(results, 'reach:worker')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('DEMO_WORKER_SSH')
  })

  it('a sidecar serving the wrong tailnet fails without retry', async () => {
    const wrong = probe({ ...HEALTHY, '7890/health': { status: 200, body: { ok: true, tailnetName: 'other.eth' } } })
    const r = byId(await run(context({ nodes: { gateway: wrong, worker: probe(HEALTHY) } })), 'sidecar:gateway')
    expect(r.status).toBe('fail')
    expect(r.retryable).toBeFalsy()
  })

  it('a connection error becomes a retryable failure with the URL scrubbed', async () => {
    const down = probe({
      ...HEALTHY,
      '7890/health': new Error('connect ECONNREFUSED https://user:pw@10.0.0.1:7890/health'),
    })
    const r = byId(await run(context({ nodes: { gateway: down, worker: probe(HEALTHY) } })), 'sidecar:gateway')
    expect(r.status).toBe('fail')
    expect(r.retryable).toBe(true)
    expect(r.detail).not.toContain('pw@')
  })

  it('reports Tor starting, then ready, when given time', async () => {
    let calls = 0
    const tor = probe({
      ...HEALTHY,
      '7890/tor/status': () => ({ status: 200, body: { state: ++calls < 3 ? 'starting' : 'ready' } }),
    })
    const ctx = context({ nodes: { gateway: tor, worker: probe(HEALTHY) } })

    const once = await run(ctx, 0)
    expect(byId(once, 'tor:gateway').detail).toContain('starting Tor')

    calls = 0
    const waited = await runPreflight(buildChecks(ctx), {
      waitMs: 1000,
      intervalMs: 10,
      now: clock(),
      sleep: async () => {},
    })
    expect(byId(waited, 'tor:gateway').status).toBe('pass')
  })

  it('a failed Tor start gives a fixed diagnostic and never echoes the sidecar error text', async () => {
    const tor = probe({
      ...HEALTHY,
      '7890/tor/status': {
        status: 200,
        body: { state: 'failed', lastError: `bootstrap failed at https://x.example/y key ${SECRET}` },
      },
    })
    const r = byId(await run(context({ nodes: { gateway: tor, worker: probe(HEALTHY) } })), 'tor:gateway')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('Tor failed to start')
    expect(r.detail).not.toContain('x.example')
    expect(r.detail).not.toContain(SECRET)
  })

  it('an old sidecar with no /tor/status fails with a clear reason', async () => {
    const old = probe({ ...HEALTHY, '7890/tor/status': { status: 404, body: {} } })
    const r = byId(await run(context({ nodes: { gateway: old, worker: probe(HEALTHY) } })), 'tor:gateway')
    expect(r.detail).toContain('predates the Tor client')
  })

  it('STUN discovery states map to results', async () => {
    const states: Array<[unknown, string]> = [
      [{ state: 'disabled' }, 'BRAMBLE_STUN_SERVERS'],
      [{ state: 'discovering' }, 'in progress'],
      [{ state: 'unavailable', error: 'no response' }, 'STUN discovery failed'],
      [{ state: 'REMOTE_OPAQUE_CREDENTIAL' }, 'unrecognised state'],
    ]
    for (const [body, text] of states) {
      const node = probe({ ...HEALTHY, '7899/endpoint': { status: 200, body } })
      const r = byId(await run(context({ nodes: { gateway: node, worker: probe(HEALTHY) } })), 'stun:gateway')
      expect(r.status).toBe('fail')
      expect(r.detail).toContain(text)
      expect(r.detail).not.toContain('REMOTE_OPAQUE_CREDENTIAL')
    }
  })

  it('fails when the agent device is not in the registry', async () => {
    const node = probe({ ...HEALTHY, '7890/devices': { status: 200, body: { labels: ['gateway'] } } })
    const r = byId(await run(context({ nodes: { gateway: node, worker: probe(HEALTHY) } })), 'registry:gateway')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('agent')
  })
})

describe('relay', () => {
  it('passes when the rendezvous address and payment sidecar answer', async () => {
    const r = byId(await run(context()), 'relay')
    expect(r.status).toBe('pass')
  })

  it('fails when nothing answers', async () => {
    const ctx = context({
      async tcp() {
        throw new Error('timeout')
      },
      async http(url) {
        if (url.includes('/api/v1/accounts/'))
          return { status: 200, body: { tokens: [{ token_id: USDC, balance: 2_000_000 }] } }
        throw new Error('connect refused')
      },
    })
    const r = byId(await run(ctx), 'relay')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('relay1')
  })

  const TWO_RELAYS = probe({
    ...HEALTHY,
    '7890/relays': {
      status: 200,
      body: {
        rendezvous: [
          { label: 'relay1', address: 'relay1.example.org:3478', sidecarUrl: 'https://pay1.example.org' },
          { label: 'relay2', address: 'relay2.example.org:3478', sidecarUrl: 'https://pay2.example.org' },
        ],
        dataRelays: [],
      },
    },
  })

  it('warns, naming the redundant relay, when one of two relays is down', async () => {
    const ctx = context({
      nodes: { gateway: TWO_RELAYS, worker: TWO_RELAYS },
      async tcp(hostPort) {
        if (hostPort.startsWith('relay2')) throw new Error('timeout')
      },
    })
    const r = byId(await run(ctx), 'relay')
    expect(r.status).toBe('warn')
    expect(r.detail).toContain('redundant relays down: relay2')
  })

  it('fails when the rendezvous relay is reachable but the only payment sidecar is unhealthy', async () => {
    const ctx = context({
      async http(url) {
        if (url.includes('/api/v1/accounts/'))
          return { status: 200, body: { tokens: [{ token_id: USDC, balance: 2_000_000 }] } }
        return { status: 503, body: {} }
      },
    })
    const r = byId(await run(ctx), 'relay')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('no payment sidecar healthy')
    expect(exitCode([r])).toBe(1)
  })

  it('fails when every rendezvous relay is down even though a payment sidecar answers', async () => {
    const ctx = context({
      async tcp() {
        throw new Error('timeout')
      },
    })
    const r = byId(await run(ctx), 'relay')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('no rendezvous relay reachable')
  })

  it('fails when the registry lists no payment sidecar', async () => {
    const bare = probe({
      ...HEALTHY,
      '7890/relays': {
        status: 200,
        body: { rendezvous: [{ label: 'r', address: 'relay.example.org:3478' }], dataRelays: [] },
      },
    })
    const r = byId(await run(context({ nodes: { gateway: bare, worker: bare } })), 'relay')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('no payment sidecar')
  })

  it('does not fetch a rendezvous address that is not host:port', async () => {
    const bad = probe({
      ...HEALTHY,
      '7890/relays': { status: 200, body: { rendezvous: [{ label: 'r', address: 'http://evil/x' }], dataRelays: [] } },
    })
    const ctx = context({ nodes: { gateway: bad, worker: bad } })
    const r = byId(await run(ctx), 'relay')
    expect(r.status).toBe('fail')
  })
})

describe('hedera payer', () => {
  function withMirror(body: unknown, status = 200): PreflightContext {
    return context({
      async http(url) {
        if (url.includes('/api/v1/accounts/')) return { status, body }
        return { status: 200, body: {} }
      },
    })
  }

  it('passes with USDC associated and enough balance', async () => {
    const r = byId(await run(context()), 'hedera:0.0.1001')
    expect(r.status).toBe('pass')
    expect(r.detail).toContain('2.000000 USDC')
  })

  it('fails when USDC is not associated', async () => {
    const r = byId(await run(withMirror({ tokens: [] })), 'hedera:0.0.1001')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('not associated')
  })

  it('fails when the balance is below the minimum', async () => {
    const r = byId(await run(withMirror({ tokens: [{ token_id: USDC, balance: 100 }] })), 'hedera:0.0.1001')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('below')
  })

  it('honours DEMO_MIN_PAYER_USDC_ATOMIC', async () => {
    const ctx = withMirror({ tokens: [{ token_id: USDC, balance: 100 }] })
    ctx.env = { ...ctx.env, DEMO_MIN_PAYER_USDC_ATOMIC: '50' }
    expect(byId(await run(ctx), 'hedera:0.0.1001').status).toBe('pass')
  })

  it('fails on an unknown account', async () => {
    expect(byId(await run(withMirror({}, 404)), 'hedera:0.0.1001').status).toBe('fail')
  })

  it('checks every account in DEMO_PAYER_ACCOUNT_IDS', async () => {
    const ctx = context()
    ctx.env = { ...ctx.env, DEMO_PAYER_ACCOUNT_IDS: '0.0.1001, 0.0.1002' }
    const ids = (await run(ctx)).map((r) => r.id).filter((id) => id.startsWith('hedera:'))
    expect(ids).toEqual(['hedera:0.0.1001', 'hedera:0.0.1002'])
  })

  it('rejects a payer id that is not shaped like a Hedera account', async () => {
    const ctx = context()
    ctx.env = { ...ctx.env, DEMO_PAYER_ACCOUNT_IDS: '../../etc/passwd' }
    const r = byId(await run(ctx), 'hedera:invalid-1')
    expect(r.status).toBe('fail')
  })

  it('hides a short known secret that a throwing probe puts in its error', async () => {
    const token = 'demo123'
    const ctx = context({
      nodes: { gateway: probe({ ...HEALTHY, '7890/health': new Error(`boom ${token}`) }), worker: probe(HEALTHY) },
    })
    ctx.env = { ...ctx.env, APPROVAL_DEMO_TOKEN: token }
    const results = await run(ctx)
    expect(formatReport(results)).not.toContain(token)
    expect(results.some((r) => r.status === 'fail')).toBe(true)
  })

  it('never prints an invalid payer value, even one that is a known secret', async () => {
    const token = 'demo123'
    const ctx = context()
    ctx.env = { ...ctx.env, APPROVAL_DEMO_TOKEN: token, DEMO_PAYER_ACCOUNT_IDS: token }
    const results = await run(ctx)
    expect(formatReport(results)).not.toContain(token)
    expect(byId(results, 'hedera:invalid-1').status).toBe('fail')
  })
})

describe('runner', () => {
  it('a throwing check becomes a failure instead of aborting the run', async () => {
    const units: CheckUnit[] = [
      {
        id: 'a',
        run: async () => {
          throw new Error('boom')
        },
      },
      { id: 'b', run: async () => ({ id: 'b', status: 'pass', detail: 'ok' }) },
    ]
    const results = await runPreflight(units, { waitMs: 0, intervalMs: 1, sleep: async () => {} })
    expect(results.map((r) => r.status)).toEqual(['fail', 'pass'])
  })

  it('keeps a failing node distinct from a healthy one when scrubbing makes their ids collide', async () => {
    const down: Routes = { '7890/relays': HEALTHY['7890/relays'] as HttpResponse }
    for (const path of ['7890/health', '7890/tor/status', '7899/endpoint', '7890/devices']) {
      down[path] = new Error('unreachable')
    }
    const ctx = context({
      env: { ...context().env, APPROVAL_DEMO_TOKEN: 'gateway', OTHER_TOKEN: 'worker' },
      localRoles: [],
      nodes: { gateway: probe(down, 'gateway'), worker: probe(HEALTHY, 'worker') },
    })
    const results = await runPreflight(buildChecks(ctx), { waitMs: 0, intervalMs: 1, sleep: async () => {} })
    const nodeResults = results.filter((r) => /^(sidecar|tor|stun):/.test(r.id))
    expect(new Set(nodeResults.map((r) => r.id)).size).toBeLessThan(nodeResults.length)
    expect(results.filter((r) => r.status === 'fail').length).toBeGreaterThanOrEqual(3)
    expect(exitCode(results)).toBe(1)
  })

  it('retries only failed retryable checks and stops at the deadline', async () => {
    const counts = { flaky: 0, stable: 0, hard: 0 }
    const units: CheckUnit[] = [
      {
        id: 'flaky',
        run: async () =>
          ++counts.flaky >= 3
            ? { id: 'flaky', status: 'pass', detail: '' }
            : { id: 'flaky', status: 'fail', detail: '', retryable: true },
      },
      {
        id: 'stable',
        run: async () => {
          counts.stable++
          return { id: 'stable', status: 'pass', detail: '' }
        },
      },
      {
        id: 'hard',
        run: async () => {
          counts.hard++
          return { id: 'hard', status: 'fail', detail: '' }
        },
      },
    ]
    const results = await runPreflight(units, { waitMs: 1000, intervalMs: 10, now: clock(), sleep: async () => {} })
    expect(results.map((r) => r.status)).toEqual(['pass', 'pass', 'fail'])
    expect(counts).toEqual({ flaky: 3, stable: 1, hard: 1 })
  })

  it('gives up retrying once the wait is spent', async () => {
    let n = 0
    const units: CheckUnit[] = [
      {
        id: 'x',
        run: async () => {
          n++
          return { id: 'x', status: 'fail', detail: 'down', retryable: true }
        },
      },
    ]
    const results = await runPreflight(units, { waitMs: 50, intervalMs: 10, now: clock(), sleep: async () => {} })
    expect(results[0].status).toBe('fail')
    expect(n).toBeGreaterThan(1)
    expect(n).toBeLessThan(10)
  })
})

describe('report', () => {
  it('lists each result and a summary, and exits 1 on a failure', () => {
    const results: CheckResult[] = [
      { id: 'a', status: 'pass', detail: 'fine' },
      { id: 'bb', status: 'warn', detail: 'hmm' },
      { id: 'c', status: 'fail', detail: 'bad' },
    ]
    const text = formatReport(results)
    expect(text).toContain('PASS  a ')
    expect(text).toContain('WARN  bb')
    expect(text).toContain('FAIL  c ')
    expect(text).toContain('1 passed, 1 warnings, 1 failed')
    expect(exitCode(results)).toBe(1)
  })

  it('formats atomic USDC', () => {
    expect(formatUsdc(500_000n)).toBe('0.500000')
    expect(formatUsdc(12_345_678n)).toBe('12.345678')
  })

  it('covers every node role', () => {
    expect(NODE_ROLES).toEqual(['gateway', 'worker'])
  })
})

/** A fake clock that advances 10 ms per read. */
function clock(): () => number {
  let t = 0
  return () => (t += 10)
}

describe('secrets never reach the report', () => {
  const KEY = '0x' + 'ab'.repeat(32)
  const PAYER_KEY = 'payer-private-key-synthetic-0001'

  it('scrubs a context secret thrown inside a check, in the unit and in the rendered report', async () => {
    const throwing: NodeProbe = {
      describe: 'worker',
      async getJson() {
        throw new Error(`ssh failed with ${PAYER_KEY} and ${KEY}`)
      },
    }
    const ctx = context({
      env: { ...context().env, HEDERA_CLIENT_PRIVATE_KEY: PAYER_KEY },
      nodes: { gateway: throwing, worker: throwing },
    })
    const text = formatReport(await run(ctx))
    expect(text).toContain('<redacted>')
    expect(text).not.toContain(PAYER_KEY)
    expect(text).not.toContain(KEY)
  })

  it('scrubs secrets that only the role env file or ambient list knows', async () => {
    const throwing: NodeProbe = {
      describe: 'worker',
      async getJson() {
        throw new Error('boom role-file-secret-value ambient-secret-value')
      },
    }
    const ctx = context({
      roleEnv: { gateway: { ...GOOD_NODE_ENV, BRAMBLE_PRIVATE_KEY: 'role-file-secret-value' } },
      secrets: ['ambient-secret-value'],
      nodes: { gateway: throwing, worker: throwing },
    })
    const text = formatReport(await run(ctx))
    expect(text).not.toContain('role-file-secret-value')
    expect(text).not.toContain('ambient-secret-value')
  })

  it('never prints remote error text from the registry check', async () => {
    const node = probe({ ...HEALTHY, '7890/devices': { status: 502, body: { error: `upstream ${SECRET}` } } })
    const r = byId(await run(context({ nodes: { gateway: node, worker: probe(HEALTHY) } })), 'registry:gateway')
    expect(r.detail).toContain('502')
    expect(r.detail).not.toContain(SECRET)
  })
})

describe('relay support units', () => {
  it('fails on the relays machine when a support unit is not active, naming the fix', async () => {
    const ctx = context({
      localRoles: ['relays'],
      env: { ...context().env, RELAY_PAYEE_ACCOUNT_ID: '0.0.9' },
      async unitActive(unit) {
        return unit !== 'coturn'
      },
    })
    const r = byId(await run(ctx), 'services:relays')
    expect(r.status).toBe('fail')
    expect(r.detail).toContain('systemctl enable --now coturn')
  })

  it('passes when all three are active, and is absent off the relays machine', async () => {
    const on = context({ localRoles: ['relays'], unitActive: async () => true })
    expect(byId(await run(on), 'services:relays').status).toBe('pass')
    const off = await run(context({ unitActive: async () => true }))
    expect(off.some((r) => r.id === 'services:relays')).toBe(false)
  })
})
