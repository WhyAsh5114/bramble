export interface AgentMcpConfig {
  approvalUrl: string
  approvalToken: string | undefined
  agentLabel: string
  gatewayLabel: string
  forwardUrl: string
  pollIntervalMs: number
  decisionTimeoutMs: number
  forwardTimeoutMs: number
}

const DEFAULTS = {
  approvalUrl: 'http://127.0.0.1:3001',
  forwardUrl: 'http://127.0.0.1:8080',
  pollIntervalMs: 1000,
  decisionTimeoutMs: 120_000,
  forwardTimeoutMs: 10_000,
}

function positiveInt(env: Record<string, string | undefined>, name: string, fallback: number): number {
  const raw = env[name]
  if (raw === undefined || raw === '') return fallback
  const value = Number(raw)
  if (!Number.isInteger(value) || value <= 0) throw new Error(`${name} must be a positive integer.`)
  return value
}

function required(env: Record<string, string | undefined>, name: string): string {
  const value = env[name]?.trim()
  if (!value) throw new Error(`${name} is required.`)
  return value
}

function loopbackUrl(raw: string): string {
  const { hostname, protocol } = new URL(raw)
  if (protocol !== 'http:') throw new Error('BRAMBLE_FORWARD_URL must use http://; the forward speaks plain HTTP.')
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(hostname)) {
    throw new Error(
      'BRAMBLE_FORWARD_URL must point at a loopback address; the forward is the only route to the service.'
    )
  }
  return raw
}

export function loadConfig(env: Record<string, string | undefined> = process.env): AgentMcpConfig {
  return {
    approvalUrl: env.BRAMBLE_APPROVAL_URL?.trim() || DEFAULTS.approvalUrl,
    approvalToken: env.APPROVAL_DEMO_TOKEN?.trim() || undefined,
    agentLabel: required(env, 'BRAMBLE_AGENT_LABEL'),
    gatewayLabel: required(env, 'BRAMBLE_GATEWAY_LABEL'),
    forwardUrl: loopbackUrl(env.BRAMBLE_FORWARD_URL?.trim() || DEFAULTS.forwardUrl),
    pollIntervalMs: positiveInt(env, 'BRAMBLE_POLL_INTERVAL_MS', DEFAULTS.pollIntervalMs),
    decisionTimeoutMs: positiveInt(env, 'BRAMBLE_DECISION_TIMEOUT_MS', DEFAULTS.decisionTimeoutMs),
    forwardTimeoutMs: positiveInt(env, 'BRAMBLE_FORWARD_TIMEOUT_MS', DEFAULTS.forwardTimeoutMs),
  }
}
