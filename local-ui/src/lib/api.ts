// Types and calls for the bramble daemon's local API (node-daemon/internal/daemon).
// The page is served by the daemon itself, so requests are same-origin.

export type Phase = 'setup' | 'syncing' | 'waiting' | 'member' | 'revoked'

export interface TailnetRef {
  name: string
  registry: `0x${string}`
  resolver: `0x${string}`
  owner: string
  start: number
}

export interface NetworkInfo {
  chainId: number
  netSuffix: string
  netRegistry: `0x${string}`
  netRegistrar: `0x${string}`
  relaySuffix: string
  relayRegistry: `0x${string}`
  relayRegistrar: `0x${string}`
  ethRegistry: `0x${string}`
  explorerTx: string
  explorerAddress: string
}

export interface ServiceInfo {
  name: string
  port: number
  open: boolean
  builtIn?: boolean
}

export interface State {
  version: string
  phase: Phase
  problem?: string
  publicKey: string
  fingerprint: string
  label?: string
  tailnet?: TailnetRef
  joinCode?: string
  chain?: { head: number; confirmed: number; lastSync: string; error?: string; endpoints: string[] }
  node?: { running: boolean; address?: string; endpoint?: string; stun?: string }
  network: NetworkInfo
  settings: { ping: boolean; relayFirst: boolean; rpcUrls: string[] }
  services: ServiceInfo[]
}

export interface Device {
  label: string
  name: string
  self: boolean
  pubkey?: string
  fingerprint?: string
  meshIp?: string
  revoked: boolean
  expired: boolean
  expiry: string
  owner: string
  grants: number
  acl: string[] | null
  granters: string[] | null
  block: number
  connected: boolean
  path?: string
  endpoint?: string
  lastSeen?: number
}

export interface JoinRequest {
  t: string
  l: string
  k: string
  fingerprint: string
  meshIp: string
  name: string
  existing?: Device
}

export interface PeerService {
  name: string
  access: 'tailnet' | 'granted'
}

export interface PingResult {
  peer: string
  millis: number
  answer: { pong?: string; time?: string; version?: string } | null
  path?: string
}

export interface Forward {
  peer: string
  service: string
  port: number
}

export interface RelayInfo {
  label: string
  name: string
  address: string
  pin: string
  dataRelay: boolean
  stun?: string
  price: string
  region?: string
  owner: string
  block: number
  millis?: number
  error?: string
  preferred?: boolean
  status?: {
    version: string
    peers: number
    sessions: number
    free: boolean
    /** Set when the relay charges for data sessions. pricePerMiB is in the asset's smallest unit. */
    paid?: { network: string; asset: string; payTo: string; pricePerMiB: number; auditTopic?: string }
    budget: { dayBytes: number; dayLimit: number; monthBytes: number; monthLimit: number; forwardingPaused: boolean }
  }
}

export interface Payment {
  relay: string
  amount: number
  asset: string
  bytes: number
  transaction: string
  at: string
}

/** Paying relays that charge for data sessions (ADR 0021). Amounts are USDC's smallest unit (six decimals). */
export interface PayInfo {
  enabled: boolean
  network: string
  asset: string
  evmAddress?: string
  account?: string
  hbarTinybar: number
  usdc: number
  active: boolean
  maxSession: number
  maxDay: number
  spentToday: number
  recent: Payment[]
  keyFile?: string
  error?: string
}

export interface ActivityEvent {
  time: string
  kind: string
  label?: string
  outcome?: string
  message: string
}

export class ApiError extends Error {}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'content-type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: 'no-store',
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const msg = data?.error?.message ?? `Bramble answered ${res.status}`
    const next = data?.error?.nextStep
    throw new ApiError(next ? `${msg} ${next}` : msg)
  }
  return data as T
}

export const api = {
  state: () => call<State>('GET', '/api/state'),
  lookup: (name: string, start = 0) => call<TailnetRef>('POST', '/api/tailnet/lookup', { name, start }),
  select: (name: string, label: string, start = 0) =>
    call<State>('POST', '/api/tailnet/select', { name, label, start }),
  leave: () => call<State>('POST', '/api/tailnet/leave'),
  devices: () => call<Device[]>('GET', '/api/devices'),
  meshIp: () => call<{ meshIp: string }>('GET', '/api/mesh-ip'),
  joinCode: (code: string) => call<JoinRequest>('POST', '/api/join-code', { code }),
  grantDigest: (device: string, gateway: string, service: string) =>
    call<{ digest: string }>('POST', '/api/grant-digest', { device, gateway, service }),
  isAdmin: (address: string) => call<{ admin: boolean }>('GET', `/api/admin?address=${encodeURIComponent(address)}`),
  setServices: (services: { name: string; port: number; open: boolean }[], ping: boolean) =>
    call<State>('PUT', '/api/services', { services, ping }),
  setSettings: (relayFirst: boolean, rpcUrls: string[]) => call<State>('PUT', '/api/settings', { relayFirst, rpcUrls }),
  peerServices: (label: string) => call<PeerService[]>('GET', `/api/peers/${encodeURIComponent(label)}/services`),
  ping: (label: string) => call<PingResult>('POST', `/api/peers/${encodeURIComponent(label)}/ping`),
  forwards: () => call<Forward[]>('GET', '/api/forwards'),
  openForward: (peer: string, service: string) => call<Forward>('POST', '/api/forwards', { peer, service }),
  closeForward: (port: number) => call<Forward[]>('DELETE', `/api/forwards/${port}`),
  activity: () => call<ActivityEvent[]>('GET', '/api/activity'),
  pay: () => call<PayInfo>('GET', '/api/pay'),
  setPay: (enabled: boolean, maxSession: number, maxDay: number) =>
    call<PayInfo>('PUT', '/api/pay', { enabled, maxSession, maxDay }),
  relays: () => call<RelayInfo[]>('GET', '/api/relays'),
}

/** Testnet USDC has six decimals. */
export function usdc(units: number) {
  const v = units / 1e6
  return `${v.toLocaleString('en', { minimumFractionDigits: 2, maximumFractionDigits: 6 })} USDC`
}

export function shortAddress(a?: string) {
  if (!a) return ''
  return `${a.slice(0, 6)}…${a.slice(-4)}`
}

export function bytes(n: number) {
  if (n < 1024) return `${n} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

export function ago(iso?: string | number) {
  if (!iso) return 'never'
  const t = typeof iso === 'number' ? iso : Date.parse(iso)
  const s = Math.max(0, Math.round((Date.now() - t) / 1000))
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  return `${Math.round(s / 3600)}h ago`
}
