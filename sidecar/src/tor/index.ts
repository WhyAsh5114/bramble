// One Tor client per sidecar, shared by every outbound call (docs/adr/0013).
//
// Interface contract other tickets build on (W9-3 wires ENS and payments):
//
//   torFetch(key)  a fetch() whose requests use the isolation key for `key`
//   getTorStatus() 'starting' | 'ready' | 'failed', with readyAt / lastError
//   startTor()     bootstraps the shared client; idempotent
//
// Isolation: each TorKey maps to its own isolation key, so ENS reads and
// payments never share a Tor circuit and an exit relay or RPC provider cannot
// link them by circuit. tor-js guarantees that different keys never share a
// circuit (Verified, 5 Nov 2026 spike).
//
// Retry: tor-js keeps using a circuit after an exit fails to open a stream,
// so the same request can keep failing for minutes. About 1 in 10 first
// requests failed in the 5 Nov spike and succeeded under a new isolation
// key. torFetch therefore retries a circuit failure once under a fresh key,
// and keeps the fresh key for later calls with the same TorKey.
import { toTorRequest } from './request'
import { createTorJsTransport, type TorTransport, type TorTransportFactory } from './transport'

export type { TorFetchInit, TorTransport, TorTransportFactory } from './transport'

export type TorKey = 'ens' | 'pay'
export const TOR_KEYS: readonly TorKey[] = ['ens', 'pay']

export type TorState = 'starting' | 'ready' | 'failed'
export interface TorStatus {
  state: TorState
  /** Epoch milliseconds at which the client became ready. */
  readyAt?: number
  /** Message of the most recent bootstrap failure. */
  lastError?: string
}

export interface StartTorOptions {
  /** Defaults to tor-js with MemoryStorage. Tests pass a fake. */
  transport?: TorTransportFactory
  /** Bootstrap deadline per attempt. Default 180 s (spike: ready after 51 s). */
  readyTimeoutMs?: number
  /** Wait before the next attempt after a failed one. Default 30 s. 0 disables. */
  retryDelayMs?: number
  /** Clock, for tests. */
  now?: () => number
}

const DEFAULT_READY_TIMEOUT_MS = 180_000
const DEFAULT_RETRY_DELAY_MS = 30_000

// Arti error kinds that are the destination's own answer. A new circuit
// reaches the same destination and gets the same answer, so retrying wastes
// a circuit build. See tor-js README, "client.fetch".
const DESTINATION_ANSWERS = new Set(['RemoteConnectionRefused'])

export class TorNotReadyError extends Error {
  readonly status: TorStatus
  constructor(status: TorStatus) {
    super(status.state === 'failed' ? `Tor failed to start: ${status.lastError ?? 'unknown error'}` : 'starting Tor')
    this.name = 'TorNotReadyError'
    this.status = status
  }
}

let transport: TorTransport | null = null
let status: TorStatus = { state: 'starting' }
let inFlight: Promise<void> | null = null
let pendingCreation: Promise<void> | null = null
let pendingClose: Promise<void> | null = null
let stopping: Promise<void> | null = null
let retryTimer: ReturnType<typeof setTimeout> | null = null
let options: StartTorOptions = {}
let configured = false
// Bumped by stopTor() so an attempt that settles after a stop cannot
// overwrite the reset state.
let generation = 0
const isolationKeys = new Map<TorKey, string>()

function freshIsolationKey(key: TorKey): string {
  // The key never leaves the client (tor-js README, "Circuit isolation"), so
  // it needs to be unique, not secret. The TorKey prefix makes logs readable.
  return `${key}:${crypto.randomUUID()}`
}

function isolationKeyFor(key: TorKey): string {
  let current = isolationKeys.get(key)
  if (current === undefined) {
    current = freshIsolationKey(key)
    isolationKeys.set(key, current)
  }
  return current
}

// Rotates `key` away from `failed`, unless a concurrent request already did.
// Returns the key to retry under.
function rotateIsolationKey(key: TorKey, failed: string): string {
  const current = isolationKeyFor(key)
  if (current !== failed) return current
  const next = freshIsolationKey(key)
  isolationKeys.set(key, next)
  return next
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** True for failures a new circuit can fix: tor-js code CONNECTION, not aborted, not the destination's answer. */
export function isCircuitFailure(err: unknown): boolean {
  if (typeof err !== 'object' || err === null) return false
  const e = err as { code?: unknown; retryable?: unknown; torKind?: unknown }
  if (e.code === 'ABORT') return false
  if (typeof e.torKind === 'string' && DESTINATION_ANSWERS.has(e.torKind)) return false
  return e.code === 'CONNECTION' || e.retryable === true
}

// The attempt's deadline passed while bootstrap was still in progress.
class BootstrapTimeoutError extends Error {}

function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new BootstrapTimeoutError(`Tor not ready after ${Math.round(ms / 1000)} s`)),
      ms
    )
    p.then(resolve, reject).finally(() => clearTimeout(timer))
  })
}

// Closing is cleanup: its own failure must not replace the error being reported.
async function closeQuietly(t: TorTransport): Promise<void> {
  try {
    await t.close()
  } catch {
    // Nothing to recover; the client is discarded either way.
  }
}

async function attempt(): Promise<void> {
  const gen = generation
  const now = options.now ?? Date.now
  status = { state: 'starting', ...(status.lastError !== undefined && { lastError: status.lastError }) }
  try {
    if (stopping !== null) {
      await stopping
      if (gen !== generation) return
    }
    if (transport === null) {
      const creating = Promise.resolve((options.transport ?? createTorJsTransport)()).then((created) => {
        if (gen !== generation) return closeQuietly(created)
        transport = created
      })
      pendingCreation = creating
      try {
        await creating
      } finally {
        if (pendingCreation === creating) pendingCreation = null
      }
      if (gen !== generation) return
    }
    // The creation promise installs the transport before it resolves.
    await withTimeout(transport!.ready(), options.readyTimeoutMs ?? DEFAULT_READY_TIMEOUT_MS)
    if (gen !== generation) return
    status = { state: 'ready', readyAt: now() }
  } catch (err) {
    if (gen !== generation) return
    // A bootstrap that failed discards its client, so the next attempt
    // bootstraps a new one. tor-js 0.4.4 starts bootstrap in the TorClient
    // constructor and caches that promise: once it rejects, every later
    // ready() on the same client rejects with the same error, so reusing it
    // could never recover. The attempt settles only once that client's relay
    // sockets are closed. Until then it stays in flight, so a concurrent
    // startTor() gets this attempt and no retry is scheduled: two clients
    // never run at once.
    //
    // A timeout keeps the client, and the next attempt waits on the same
    // bootstrap. A pending bootstrap is not stuck: Arti keeps retrying relays
    // with backoff. tor-js cannot cancel it either, so a replacement would
    // leave the old bootstrap retrying in the WASM, one more per timeout
    // (Verified 6 Nov 2026: a closed client's bootstrap kept trying 2 to 3
    // relay dials per second for the two minutes measured).
    if (!(err instanceof BootstrapTimeoutError)) {
      const failed = transport
      transport = null
      if (failed !== null) {
        const closing = closeQuietly(failed)
        pendingClose = closing
        await closing
        if (pendingClose === closing) pendingClose = null
      }
      if (gen !== generation) return
    }
    status = { state: 'failed', lastError: messageOf(err) }
    scheduleRetry()
    throw err
  }
}

function scheduleRetry(): void {
  const delay = options.retryDelayMs ?? DEFAULT_RETRY_DELAY_MS
  if (delay <= 0 || retryTimer !== null) return
  retryTimer = setTimeout(() => {
    retryTimer = null
    // The failure is recorded in status; nobody awaits a background retry.
    startTor().catch(() => {})
  }, delay)
}

/**
 * Bootstraps the sidecar's single Tor client. Resolves when it is ready and
 * rejects if this attempt fails; a failed attempt schedules another after
 * `retryDelayMs`. After a bootstrap error the next attempt uses a new client;
 * after a timeout it keeps waiting on the same one. Calling it again while
 * starting returns the same promise;
 * calling it when ready resolves at once. Options apply from the first call
 * until stopTor().
 */
export function startTor(opts?: StartTorOptions): Promise<void> {
  if (!configured) {
    options = opts ?? {}
    configured = true
  }
  if (status.state === 'ready') return Promise.resolve()
  if (inFlight !== null) return inFlight
  if (retryTimer !== null) {
    clearTimeout(retryTimer)
    retryTimer = null
  }
  const p = attempt().finally(() => {
    if (inFlight === p) inFlight = null
  })
  inFlight = p
  return p
}

export function getTorStatus(): TorStatus {
  return { ...status }
}

/**
 * Returns a fetch() that sends every request through the shared Tor client
 * under `key`'s isolation key. It rejects with TorNotReadyError until
 * startTor() has finished, instead of waiting up to a minute for bootstrap
 * or falling back to a direct connection. A circuit failure is retried once
 * under a fresh isolation key.
 */
export function torFetch(key: TorKey): typeof fetch {
  const fetchViaTor = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const client = transport
    if (status.state !== 'ready' || client === null) throw new TorNotReadyError(getTorStatus())
    const { url, init: torInit } = await toTorRequest(input, init)

    const firstKey = isolationKeyFor(key)
    try {
      return await client.fetch(url, { ...torInit, anonOptions: { isolationKey: firstKey } })
    } catch (err) {
      if (!isCircuitFailure(err) || torInit.signal?.aborted) throw err
      const retryKey = rotateIsolationKey(key, firstKey)
      return await client.fetch(url, { ...torInit, anonOptions: { isolationKey: retryKey } })
    }
  }
  // Bun's fetch type also carries a `preconnect` helper; no caller here uses it.
  return fetchViaTor as typeof fetch
}

/**
 * Closes the client and resets all state. Used on shutdown and between tests.
 * The state resets at once; the returned promise resolves when the client's
 * relay sockets are closed.
 */
export function stopTor(): Promise<void> {
  generation++
  if (retryTimer !== null) clearTimeout(retryTimer)
  retryTimer = null
  inFlight = null
  // An attempt can already be closing a rejected client after clearing
  // `transport`. Keep that cleanup visible to shutdown and later starts.
  const closing = Promise.all([
    stopping,
    pendingClose,
    // A late factory result closes itself after seeing the new generation.
    pendingCreation?.catch(() => {}),
    transport === null ? null : closeQuietly(transport),
  ]).then(() => {})
  const stopped = closing.finally(() => {
    if (stopping === stopped) stopping = null
  })
  stopping = stopped
  transport = null
  status = { state: 'starting' }
  options = {}
  configured = false
  isolationKeys.clear()
  return stopped
}
