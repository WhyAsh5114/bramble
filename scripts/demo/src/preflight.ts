// Preflight: one check per fact the live demo depends on. Each check is an
// independent unit that returns one result. The runner re-runs only the failed
// retryable units until a deadline, because Tor needs about a minute to start.
//
// Everything outside this file (HTTP, TCP, ssh, env files) arrives through
// PreflightContext, so unit tests use fakes. The checks print names, states
// and public identifiers. They never print an env value or a URL.
import { HEDERA_USDC_ASSET_ID } from '../../../sidecar/src/payments/config'
import { errorText, scrub, secretValues } from './redact'
import { NODE_ROLES, RELAY_SUPPORT_UNITS, SIDECAR_PORT, STATUS_API_PORT, type NodeRole, type Role } from './topology'

export type Status = 'pass' | 'warn' | 'fail'

export interface CheckResult {
  id: string
  status: Status
  detail: string
  /** A failed check that can pass later without a human, such as "starting Tor". */
  retryable?: boolean
}

export interface HttpResponse {
  status: number
  body: unknown
}

/** Reaches one node's loopback services: locally, over ssh, or inside its NAT box. */
export interface NodeProbe {
  describe: string
  getJson(port: number, path: string): Promise<HttpResponse>
}

export type Env = Record<string, string | undefined>

export interface PreflightContext {
  /** Process env plus the repo `.env`. Source of DEMO_* settings and public ids. */
  env: Env
  /** The roles this machine runs. */
  localRoles: Role[]
  /** The env a local role's service sees, when it differs from `env` (for example /etc/bramble/gateway.env). */
  roleEnv: Partial<Record<Role, Env>>
  /** A way to reach each node's loopback services. A node with no entry cannot be checked. */
  nodes: Partial<Record<NodeRole, NodeProbe>>
  http(url: string, timeoutMs: number): Promise<HttpResponse>
  tcp(hostPort: string, timeoutMs: number): Promise<void>
  /** True when the Key Ring file that holds the granter key exists. */
  granterRingPresent: boolean
  /** Secret values known on this machine, beyond what `env` and `roleEnv` hold. Scrubbed from every printed line. */
  secrets?: string[]
  /** True when systemd reports a unit active on this machine. Without it the support-unit check is skipped. */
  unitActive?(unit: string): Promise<boolean>
}

export interface CheckUnit {
  id: string
  run(): Promise<CheckResult>
}

const NODE_ENV = [
  'BRAMBLE_NODE_LABEL',
  'BRAMBLE_TAILNET_NAME',
  'BRAMBLE_TAILNET_REGISTRY',
  'BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK',
  'BRAMBLE_RELAY_REGISTRY',
  'BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK',
  'BRAMBLE_PRIVATE_KEY',
  'HEDERA_CLIENT_ACCOUNT_ID',
  'HEDERA_CLIENT_PRIVATE_KEY',
  'BRAMBLE_SERVICE_NAME',
]

const ADMIN_ENV = [
  'BRAMBLE_TAILNET_NAME',
  'BRAMBLE_TAILNET_REGISTRY',
  'BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK',
  'BRAMBLE_RELAY_REGISTRY',
  'BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK',
  'SEPOLIA_PRIVATE_KEY',
  'APPROVAL_DEMO_TOKEN',
]

/** Required env names per role (deploy/REFERENCE.md). The granter and HEDERA_SERVER_* rules are separate. */
export const REQUIRED_ENV: Record<Role, string[]> = {
  relays: ['RELAY_PAYEE_ACCOUNT_ID'],
  gateway: NODE_ENV,
  worker: [...NODE_ENV, 'BRAMBLE_GATEWAY_LABEL'],
  admin: ADMIN_ENV,
}

/** The admin machine needs at least one variable with this prefix. The exact names are not defined in the repository yet. */
export const HEDERA_SERVER_PREFIX = 'HEDERA_SERVER_'

const DEFAULT_MIRROR_URL = 'https://testnet.mirrornode.hedera.com'
/** 0.5 USDC, in atomic units. The per-payment cap is 0.10 USDC, so this covers several runs. */
const DEFAULT_MIN_PAYER_ATOMIC = 500_000n

function present(env: Env, name: string): boolean {
  return (env[name] ?? '').trim() !== ''
}

function envFor(ctx: PreflightContext, role: Role): Env {
  return ctx.roleEnv[role] ?? ctx.env
}

/** Names missing for a role, plus `HEDERA_SERVER_*` and the granter key where they apply. Never returns a value. */
export function missingEnvNames(ctx: PreflightContext, role: Role): string[] {
  const env = envFor(ctx, role)
  const missing = REQUIRED_ENV[role].filter((name) => !present(env, name))
  if (role === 'admin') {
    if (!present(env, 'BRAMBLE_GRANTER_PRIVATE_KEY') && !ctx.granterRingPresent)
      missing.push('BRAMBLE_GRANTER_PRIVATE_KEY')
    const hasServer = Object.keys(env).some((name) => name.startsWith(HEDERA_SERVER_PREFIX) && present(env, name))
    if (!hasServer) missing.push(`${HEDERA_SERVER_PREFIX}*`)
  }
  return missing
}

function fail(id: string, detail: string, retryable = false): CheckResult {
  return { id, status: 'fail', detail, retryable }
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

function envUnit(ctx: PreflightContext, role: Role): CheckUnit {
  const id = `env:${role}`
  return {
    id,
    async run() {
      const missing = missingEnvNames(ctx, role)
      if (missing.length > 0) return fail(id, `missing: ${missing.join(', ')}`)
      const extra =
        role === 'admin' && !present(envFor(ctx, role), 'BRAMBLE_GRANTER_PRIVATE_KEY')
          ? ' (granter key: Key Ring file)'
          : ''
      return { id, status: 'pass', detail: `${REQUIRED_ENV[role].length} required names set${extra}` }
    },
  }
}

function sepoliaRpcUnit(ctx: PreflightContext): CheckUnit {
  const id = 'sepolia-rpc'
  return {
    id,
    async run() {
      const set =
        ctx.localRoles.some((role) => present(envFor(ctx, role), 'SEPOLIA_RPC_URL')) ||
        present(ctx.env, 'SEPOLIA_RPC_URL')
      if (set) return { id, status: 'pass', detail: 'SEPOLIA_RPC_URL set (value not shown)' }
      return {
        id,
        status: 'warn',
        detail: 'SEPOLIA_RPC_URL unset: using the default public endpoint, so reads may occasionally be stale',
      }
    },
  }
}

function nodeUnits(ctx: PreflightContext, role: NodeRole): CheckUnit[] {
  const probe = ctx.nodes[role]
  const upper = role.toUpperCase()
  if (!probe) {
    return [
      {
        id: `reach:${role}`,
        async run() {
          return fail(
            `reach:${role}`,
            `no way to reach the ${role} node: run on that machine, or set DEMO_${upper}_SSH to an ssh target`
          )
        },
      },
    ]
  }
  const where = probe.describe

  const sidecar: CheckUnit = {
    id: `sidecar:${role}`,
    async run() {
      const id = `sidecar:${role}`
      const res = await probe.getJson(SIDECAR_PORT, '/health')
      const body = asRecord(res.body)
      if (res.status !== 200 || body.ok !== true) return fail(id, `${where}: /health answered ${res.status}`, true)
      const expected = ctx.env.BRAMBLE_TAILNET_NAME
      if (expected && body.tailnetName !== expected) {
        return fail(id, `${where}: sidecar serves tailnet ${String(body.tailnetName)}, expected ${expected}`)
      }
      return { id, status: 'pass', detail: `${where}: healthy, tailnet ${String(body.tailnetName)}` }
    },
  }

  const tor: CheckUnit = {
    id: `tor:${role}`,
    async run() {
      const id = `tor:${role}`
      const res = await probe.getJson(SIDECAR_PORT, '/tor/status')
      if (res.status === 404) return fail(id, `${where}: no /tor/status; the sidecar predates the Tor client`)
      const body = asRecord(res.body)
      if (res.status !== 200) return fail(id, `${where}: /tor/status answered ${res.status}`, true)
      if (body.state === 'ready') return { id, status: 'pass', detail: `${where}: Tor ready` }
      if (body.state === 'failed')
        return fail(id, `${where}: Tor failed to start; read the sidecar log on that node`, true)
      return fail(id, `${where}: starting Tor`, true)
    },
  }

  const stun: CheckUnit = {
    id: `stun:${role}`,
    async run() {
      const id = `stun:${role}`
      const res = await probe.getJson(STATUS_API_PORT, '/endpoint')
      if (res.status === 404) return fail(id, `${where}: no /endpoint; brambled predates STUN discovery`)
      const body = asRecord(res.body)
      if (res.status !== 200) return fail(id, `${where}: /endpoint answered ${res.status}`, true)
      switch (body.state) {
        case 'discovered':
          return {
            id,
            status: 'pass',
            detail: `${where}: public endpoint ${String(body.endpoint)} via ${String(body.server)}`,
          }
        case 'discovering':
          return fail(id, `${where}: STUN discovery in progress`, true)
        case 'disabled':
          return fail(id, `${where}: STUN discovery disabled; set BRAMBLE_STUN_SERVERS`)
        case 'unavailable':
          return fail(id, `${where}: STUN discovery failed; read the brambled log on that node`, true)
        default:
          return fail(id, `${where}: STUN discovery reported an unrecognised state`, true)
      }
    },
  }

  const registry: CheckUnit = {
    id: `registry:${role}`,
    async run() {
      const id = `registry:${role}`
      const res = await probe.getJson(SIDECAR_PORT, '/devices')
      if (res.status !== 200) {
        return fail(id, `${where}: /devices answered ${res.status}`, true)
      }
      const labels = asRecord(res.body).labels
      if (!Array.isArray(labels) || labels.length === 0)
        return fail(id, `${where}: device registry lists no devices`, true)
      const agent = ctx.env.DEMO_AGENT_LABEL
      if (agent && !labels.includes(agent)) return fail(id, `${where}: device registry does not list agent "${agent}"`)
      return { id, status: 'pass', detail: `${where}: registry resolves, ${labels.length} devices` }
    },
  }

  return [sidecar, tor, stun, registry]
}

function isHostPort(value: string): boolean {
  return /^[A-Za-z0-9.-]+:\d{1,5}$/.test(value)
}

function httpUrl(value: string): URL | undefined {
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url : undefined
  } catch {
    return undefined
  }
}

interface RelayLists {
  rendezvous: Array<{ label: string; address: string; sidecarUrl?: string }>
  dataRelays: Array<{ label: string; sidecarUrl: string }>
}

function relayUnit(ctx: PreflightContext): CheckUnit {
  const id = 'relay'
  return {
    id,
    async run() {
      const probes = NODE_ROLES.map((r) => ctx.nodes[r]).filter((p): p is NodeProbe => p !== undefined)
      if (probes.length === 0) return fail(id, 'no node to ask for the relay list')
      let lists: RelayLists | undefined
      let lastProblem = 'no node answered'
      for (const probe of probes) {
        const res = await probe.getJson(SIDECAR_PORT, '/relays')
        if (res.status === 200) {
          lists = res.body as RelayLists
          break
        }
        lastProblem = `${probe.describe}: /relays answered ${res.status}`
      }
      if (!lists) return fail(id, lastProblem, true)

      const rendezvous = Array.isArray(lists.rendezvous) ? lists.rendezvous : []
      const dataRelays = Array.isArray(lists.dataRelays) ? lists.dataRelays : []
      if (rendezvous.length === 0) return fail(id, 'the relay registry lists no rendezvous relay')

      // The paid demo path needs one reachable rendezvous relay AND one healthy payment sidecar.
      // Extra relays of either kind are redundancy: losing one is a warning, losing all is a failure.
      const rendezvousUp: string[] = []
      const rendezvousDown: string[] = []
      for (const relay of rendezvous) {
        let ok = isHostPort(relay.address)
        if (ok) {
          try {
            await ctx.tcp(relay.address, 5000)
          } catch {
            ok = false
          }
        }
        ;(ok ? rendezvousUp : rendezvousDown).push(relay.label)
      }
      const sidecarUrls = new Map<string, string>()
      for (const relay of rendezvous) if (relay.sidecarUrl) sidecarUrls.set(relay.sidecarUrl, relay.label)
      for (const relay of dataRelays) sidecarUrls.set(relay.sidecarUrl, relay.label)
      const sidecarUp: string[] = []
      const sidecarDown: string[] = []
      for (const [raw, label] of sidecarUrls) {
        const base = httpUrl(raw)
        let ok = false
        if (base) {
          try {
            ok = (await ctx.http(new URL('/health', base).toString(), 5000)).status === 200
          } catch {
            ok = false
          }
        }
        ;(ok ? sidecarUp : sidecarDown).push(label)
      }
      const list = (labels: string[]) => (labels.length > 0 ? labels.join(', ') : 'none')
      if (rendezvousUp.length === 0) {
        return fail(id, `no rendezvous relay reachable (down: ${list(rendezvousDown)})`, true)
      }
      if (sidecarUrls.size === 0) return fail(id, 'the relay registry lists no payment sidecar')
      if (sidecarUp.length === 0) {
        return fail(id, `no payment sidecar healthy (down: ${list(sidecarDown)}); paid access cannot start`, true)
      }
      const detail = `rendezvous up: ${list(rendezvousUp)}; payment sidecar up: ${list(sidecarUp)}`
      if (rendezvousDown.length > 0 || sidecarDown.length > 0) {
        return {
          id,
          status: 'warn',
          detail: `${detail}; redundant relays down: ${list([...rendezvousDown, ...sidecarDown])}`,
        }
      }
      return { id, status: 'pass', detail }
    },
  }
}

function relaySupportUnit(unitActive: (unit: string) => Promise<boolean>): CheckUnit {
  const id = 'services:relays'
  return {
    id,
    async run() {
      const down: string[] = []
      for (const unit of RELAY_SUPPORT_UNITS) if (!(await unitActive(unit))) down.push(unit)
      if (down.length > 0) {
        return fail(
          id,
          `not active: ${down.join(', ')}; start with: sudo systemctl enable --now ${down.join(' ')}`,
          true
        )
      }
      return { id, status: 'pass', detail: `active: ${RELAY_SUPPORT_UNITS.join(', ')}` }
    },
  }
}

/** Formats atomic USDC (6 decimals) as a decimal string. */
export function formatUsdc(atomic: bigint): string {
  const whole = atomic / 1_000_000n
  const frac = (atomic % 1_000_000n).toString().padStart(6, '0')
  return `${whole}.${frac}`
}

/** The payer accounts to check: DEMO_PAYER_ACCOUNT_IDS, else each local node's HEDERA_CLIENT_ACCOUNT_ID. */
export function payerAccounts(ctx: PreflightContext): string[] {
  const listed = (ctx.env.DEMO_PAYER_ACCOUNT_IDS ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
  if (listed.length > 0) return [...new Set(listed)]
  const ids = new Set<string>()
  for (const role of NODE_ROLES) {
    const id = (ctx.roleEnv[role] ?? ctx.env).HEDERA_CLIENT_ACCOUNT_ID
    if (id && ctx.localRoles.includes(role)) ids.add(id.trim())
  }
  if (ids.size === 0 && present(ctx.env, 'HEDERA_CLIENT_ACCOUNT_ID'))
    ids.add((ctx.env.HEDERA_CLIENT_ACCOUNT_ID as string).trim())
  return [...ids]
}

function minPayerAtomic(env: Env): bigint {
  const raw = env.DEMO_MIN_PAYER_USDC_ATOMIC
  return raw && /^\d+$/.test(raw) ? BigInt(raw) : DEFAULT_MIN_PAYER_ATOMIC
}

function payerUnit(ctx: PreflightContext, account: string, index: number): CheckUnit {
  const valid = /^\d+\.\d+\.\d+$/.test(account)
  // An unvalidated value can be a secret: it must not become a printed identifier.
  const id = valid ? `hedera:${account}` : `hedera:invalid-${index + 1}`
  return {
    id,
    async run() {
      if (!valid) return fail(id, 'not a Hedera account id (expected 0.0.N)')
      const mirror = ctx.env.HEDERA_MIRROR_URL || DEFAULT_MIRROR_URL
      const url = `${mirror.replace(/\/+$/, '')}/api/v1/accounts/${account}/tokens?token.id=${HEDERA_USDC_ASSET_ID}`
      const res = await ctx.http(url, 8000)
      if (res.status === 404) return fail(id, 'account not found on the mirror node')
      if (res.status !== 200) return fail(id, `mirror node answered ${res.status}`, true)
      const tokens = asRecord(res.body).tokens
      const entry = Array.isArray(tokens)
        ? tokens.map(asRecord).find((t) => t.token_id === HEDERA_USDC_ASSET_ID)
        : undefined
      if (!entry) return fail(id, `${account} is not associated with USDC ${HEDERA_USDC_ASSET_ID}`)
      const balance = BigInt(typeof entry.balance === 'number' || typeof entry.balance === 'string' ? entry.balance : 0)
      const min = minPayerAtomic(ctx.env)
      if (balance < min)
        return fail(id, `${account} holds ${formatUsdc(balance)} USDC, below the ${formatUsdc(min)} minimum`)
      return { id, status: 'pass', detail: `${account} associated, ${formatUsdc(balance)} USDC` }
    },
  }
}

/** Builds every check for this machine and the nodes it can reach. Order is the report order. */
export function buildChecks(ctx: PreflightContext): CheckUnit[] {
  const units: CheckUnit[] = []
  for (const role of ctx.localRoles) units.push(envUnit(ctx, role))
  units.push(sepoliaRpcUnit(ctx))
  for (const role of NODE_ROLES) units.push(...nodeUnits(ctx, role))
  units.push(relayUnit(ctx))
  if (ctx.localRoles.includes('relays') && ctx.unitActive) units.push(relaySupportUnit(ctx.unitActive))
  const payers = payerAccounts(ctx)
  if (payers.length === 0) {
    units.push({
      id: 'hedera',
      run: async () => fail('hedera', 'no payer account: set HEDERA_CLIENT_ACCOUNT_ID or DEMO_PAYER_ACCOUNT_IDS'),
    })
  }
  payers.forEach((account, index) => units.push(payerUnit(ctx, account, index)))
  return units.map((unit) => sanitized(ctx, unit))
}

/** Every secret value this context knows, for scrubbing. */
export function contextSecrets(ctx: PreflightContext): string[] {
  return [...(ctx.secrets ?? []), ...secretValues(ctx.env, ...Object.values(ctx.roleEnv))]
}

/** Scrubs a unit's detail and any error it throws, so no check can print a secret or a URL. */
function sanitized(ctx: PreflightContext, unit: CheckUnit): CheckUnit {
  const secrets = contextSecrets(ctx)
  const id = scrub(unit.id, secrets)
  return {
    id,
    async run() {
      try {
        const result = await unit.run()
        return { ...result, id, detail: scrub(result.detail, secrets) }
      } catch (err) {
        return fail(id, errorText(err, secrets), true)
      }
    },
  }
}

export interface RunOptions {
  /** Keep re-running failed retryable checks for this long. 0 runs each check once. */
  waitMs: number
  intervalMs: number
  now?: () => number
  sleep?: (ms: number) => Promise<void>
  onRetry?: (pendingIds: string[], remainingMs: number) => void
}

async function runUnit(unit: CheckUnit): Promise<CheckResult> {
  try {
    return await unit.run()
  } catch (err) {
    return fail(unit.id, errorText(err), true)
  }
}

export async function runPreflight(units: CheckUnit[], opts: RunOptions): Promise<CheckResult[]> {
  const now = opts.now ?? Date.now
  const sleep = opts.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)))
  const deadline = now() + opts.waitMs
  const results = new Map<CheckUnit, CheckResult>()

  let pending = units
  for (;;) {
    const round = await Promise.all(pending.map(runUnit))
    pending.forEach((u, i) => results.set(u, round[i] as CheckResult))
    const retry = units.filter((u) => {
      const r = results.get(u)
      return r?.status === 'fail' && r.retryable === true
    })
    if (retry.length === 0 || now() + opts.intervalMs > deadline) break
    opts.onRetry?.(
      retry.map((u) => u.id),
      deadline - now()
    )
    await sleep(opts.intervalMs)
    pending = retry
  }
  return units.map((u) => results.get(u) as CheckResult)
}

export function formatReport(results: CheckResult[]): string {
  const label: Record<Status, string> = { pass: 'PASS', warn: 'WARN', fail: 'FAIL' }
  const width = Math.max(...results.map((r) => r.id.length), 0)
  const lines = results.map((r) => `${label[r.status]}  ${r.id.padEnd(width)}  ${r.detail}`)
  const count = (s: Status) => results.filter((r) => r.status === s).length
  lines.push('', `${count('pass')} passed, ${count('warn')} warnings, ${count('fail')} failed`)
  return lines.join('\n')
}

export function exitCode(results: CheckResult[]): number {
  return results.some((r) => r.status === 'fail') ? 1 : 0
}
