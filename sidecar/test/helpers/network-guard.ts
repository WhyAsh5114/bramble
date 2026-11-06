// Records and blocks the JS-visible network entry points listed in the
// "Covered" block below, except the global fetch, which is replaced by a
// caller-supplied stub. Evidence that code under test reaches the network only
// through the fetch it was handed, within those limits. It is not a sandbox.
//
// Covered: net.Socket#connect (TCP/TLS clients built on it), dgram send/bind/
// connect, dns lookup/resolve/reverse (callback, promises, and both Resolver
// classes), WebSocket, Bun.connect/udpSocket/dns.lookup/fetch. On Node also
// worker_threads Worker and child_process spawn/exec/fork, including named
// imports. On Bun these two are covered only through the default export
// object: Bun's syncBuiltinESMExports does nothing, so named imports
// (static or dynamic) of Worker or execFile reach the original (Verified,
// ETH-81, Bun 1.4.0).
// Not covered: native addons, WASI or other host-provided I/O, anything that
// captured a reference to an original function before the guard installed,
// and listeners (net.Server#listen, dgram bind through a pre-bound socket).
// A clean run therefore shows no use of the covered paths, not no packets.
import childProcess from 'node:child_process'
import dgram from 'node:dgram'
import dns from 'node:dns'
import module from 'node:module'
import net from 'node:net'
import workerThreads from 'node:worker_threads'

export type GuardAttempt = { kind: string; detail: string }

type Restore = () => void

export type NetworkGuard = {
  attempts: GuardAttempt[]
  fetchCalls: Request[]
  restore: () => void
}

function describeArgs(args: unknown[]): string {
  return args
    .map((a) => {
      if (typeof a === 'string') return a
      if (typeof a === 'number') return String(a)
      if (a && typeof a === 'object') {
        const o = a as { host?: unknown; hostname?: unknown; port?: unknown; path?: unknown; address?: unknown }
        const host = o.host ?? o.hostname ?? o.address ?? o.path
        return host !== undefined ? `${String(host)}${o.port !== undefined ? `:${String(o.port)}` : ''}` : ''
      }
      return ''
    })
    .filter(Boolean)
    .join(' ')
}

export function installNetworkGuard(fetchStub: (request: Request) => Promise<Response>): NetworkGuard {
  const attempts: GuardAttempt[] = []
  const fetchCalls: Request[] = []
  const restores: Restore[] = []

  const block = (kind: string, args: unknown[]): never => {
    attempts.push({ kind, detail: describeArgs(args) })
    throw new Error(`network-guard: blocked ${kind}`)
  }

  const patch = (target: object, key: string, kind: string) => {
    if (!target) return
    const holder = target as Record<string, unknown>
    const original = holder[key]
    if (typeof original !== 'function') return
    try {
      holder[key] = (...args: unknown[]) => block(`${kind}.${key}`, args)
    } catch {
      return // read-only on this runtime (Bun); the lower-level hooks still apply
    }
    restores.push(() => {
      holder[key] = original
    })
  }

  // The TCP and TLS clients built on net.Socket (net.connect, tls.connect, http, http2, grpc-js) end in Socket#connect.
  const socketConnect = net.Socket.prototype.connect
  net.Socket.prototype.connect = function (...args: unknown[]) {
    return block('net.Socket.connect', args)
  } as typeof net.Socket.prototype.connect
  restores.push(() => {
    net.Socket.prototype.connect = socketConnect
  })

  for (const key of ['send', 'bind', 'connect']) patch(dgram.Socket.prototype, key, 'dgram.Socket')

  for (const key of Object.keys(dns)) {
    if (/^(lookup|resolve|reverse)/.test(key)) patch(dns, key, 'dns')
  }
  for (const key of Object.keys(dns.promises)) {
    if (/^(lookup|resolve|reverse)/.test(key)) patch(dns.promises, key, 'dns.promises')
  }
  for (const key of Object.getOwnPropertyNames(dns.Resolver.prototype)) {
    if (/^(resolve|reverse)/.test(key)) patch(dns.Resolver.prototype, key, 'dns.Resolver')
  }

  for (const key of Object.getOwnPropertyNames(dns.promises.Resolver.prototype)) {
    if (/^(resolve|reverse)/.test(key)) patch(dns.promises.Resolver.prototype, key, 'dns.promises.Resolver')
  }

  for (const key of ['spawn', 'spawnSync', 'exec', 'execSync', 'execFile', 'execFileSync', 'fork']) {
    patch(childProcess, key, 'child_process')
  }
  const OriginalWorker = workerThreads.Worker
  try {
    workerThreads.Worker = function (...args: unknown[]) {
      return block('worker_threads.Worker', args)
    } as unknown as typeof workerThreads.Worker
    restores.push(() => {
      workerThreads.Worker = OriginalWorker
    })
  } catch {
    // read-only on this runtime
  }

  const networkGlobals = globalThis as unknown as { fetch: typeof fetch; WebSocket?: typeof WebSocket }
  const originalFetch = networkGlobals.fetch
  networkGlobals.fetch = ((input: ConstructorParameters<typeof Request>[0], init?: RequestInit) => {
    const request = new Request(input, init) as unknown as Request
    fetchCalls.push(request.clone() as unknown as Request)
    return fetchStub(request)
  }) as typeof fetch
  restores.push(() => {
    networkGlobals.fetch = originalFetch
  })

  const originalWebSocket = networkGlobals.WebSocket
  if (originalWebSocket) {
    networkGlobals.WebSocket = function (...args: unknown[]) {
      return block('WebSocket', args)
    } as unknown as typeof WebSocket
    restores.push(() => {
      networkGlobals.WebSocket = originalWebSocket
    })
  }

  const bun = (globalThis as { Bun?: Record<string, unknown> }).Bun
  if (bun) {
    for (const key of ['connect', 'udpSocket']) patch(bun, key, 'Bun')
    if (bun.dns && typeof bun.dns === 'object') patch(bun.dns, 'lookup', 'Bun.dns')
    patch(bun, 'fetch', 'Bun')
  }

  module.syncBuiltinESMExports()

  return {
    attempts,
    fetchCalls,
    restore: () => {
      for (const restore of restores.reverse()) restore()
      module.syncBuiltinESMExports()
    },
  }
}

// A leftover Timeout is how the Hedera SDK's 24 h address-book refresh would show up (Client#close clears it).
const NETWORK_HANDLE_TYPES = new Set([
  'TCPSocketWrap',
  'TLSWRAP',
  'UDPWrap',
  'TCPConnectWrap',
  'GetAddrInfoReqWrap',
  'Timeout',
])

// Node only: network handles and timers still open on the event loop.
export function activeNetworkHandles(): string[] {
  const info = (process as { getActiveResourcesInfo?: () => string[] }).getActiveResourcesInfo
  return info ? info.call(process).filter((name) => NETWORK_HANDLE_TYPES.has(name)) : []
}
