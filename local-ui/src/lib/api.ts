import {
  parseJoins,
  parseState,
  parseTestResult,
  type BrambleState,
  type JoinRecord,
  type TestResult,
} from './contract'

/** A failed call with the message to show and what to do next. */
export class ApiFailure extends Error {
  constructor(
    message: string,
    readonly nextStep: string,
    readonly status = 0
  ) {
    super(message)
  }
}

const unreachable = new ApiFailure(
  'Bramble is not answering on this device.',
  'Check that it is running with `bramble status`, then reload this page.'
)

async function readBody(response: Response): Promise<unknown> {
  try {
    return await response.json()
  } catch {
    return null
  }
}

function failureFrom(status: number, body: unknown, fallback: ApiFailure): ApiFailure {
  const error = typeof body === 'object' && body !== null ? (body as { error?: unknown }).error : undefined
  if (typeof error === 'string') return new ApiFailure(error, fallback.nextStep, status)
  if (typeof error === 'object' && error !== null) {
    const { message, nextStep } = error as { message?: unknown; nextStep?: unknown }
    if (typeof message === 'string') {
      return new ApiFailure(message, typeof nextStep === 'string' ? nextStep : fallback.nextStep, status)
    }
  }
  return new ApiFailure(fallback.message, fallback.nextStep, status)
}

/** Ordinary calls answer at once; the connection test may wait on ENS authorization over Tor. */
const DEFAULT_TIMEOUT_MS = 10_000
/** The daemon allows 95 s for authorization over Tor, so the page waits slightly longer. */
export const TEST_TIMEOUT_MS = 100_000

async function call(
  path: string,
  init: RequestInit | undefined,
  fallback: ApiFailure,
  timeoutMs = DEFAULT_TIMEOUT_MS
): Promise<unknown> {
  let response: Response
  try {
    response = await fetch(path, {
      cache: 'no-store',
      ...init,
      signal: AbortSignal.any([...(init?.signal ? [init.signal] : []), AbortSignal.timeout(timeoutMs)]),
    })
  } catch {
    throw unreachable
  }
  const body = await readBody(response)
  if (!response.ok) throw failureFrom(response.status, body, fallback)
  return body
}

const post = (path: string, body?: unknown, timeoutMs?: number) =>
  call(
    path,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    },
    new ApiFailure('Bramble could not do that.', 'Try again. If it keeps failing, run `bramble status` in a terminal.'),
    timeoutMs
  )

export async function fetchState(signal?: AbortSignal): Promise<BrambleState> {
  const body = await call('/api/state', { signal }, unreachable)
  try {
    return parseState(body)
  } catch {
    throw new ApiFailure(
      'Bramble sent a reply this page does not understand.',
      'Update Bramble, then reload this page.'
    )
  }
}

export const setupDevice = (name: string) => post('/api/setup', { name })
export const joinNetwork = (tailnet: string) => post('/api/join', { tailnet })
export const leaveNetwork = () => post('/api/leave')

export async function requestAccess(peer: string, service: string): Promise<void> {
  await post('/api/request-access', { peer, service }, TEST_TIMEOUT_MS)
}

export async function testConnection(peer?: string): Promise<TestResult> {
  return parseTestResult(await post('/api/test', peer ? { peer } : {}, TEST_TIMEOUT_MS))
}

const adminFallback = (status?: number) =>
  status === 401 || status === 403
    ? new ApiFailure(
        'The admin sign-in was not accepted.',
        'Run `bramble admin login` again with a current admin token.',
        status
      )
    : new ApiFailure(
        'The approval service is not answering.',
        'Check your internet connection. Decisions stay paused until it answers.'
      )

async function adminCall(path: string, init?: RequestInit): Promise<unknown> {
  try {
    return await call(`/api/admin/${path}`, init, adminFallback())
  } catch (failure) {
    // A proxy error body is not written for people, so auth and server failures get fixed copy.
    if (failure instanceof ApiFailure && (failure.status === 401 || failure.status === 403 || failure.status >= 500)) {
      throw adminFallback(failure.status)
    }
    throw failure
  }
}

export async function fetchJoins(signal?: AbortSignal): Promise<JoinRecord[]> {
  return parseJoins(await adminCall('joins', { signal }), 'joins')
}

export async function fetchMembers(signal?: AbortSignal): Promise<JoinRecord[]> {
  return parseJoins(await adminCall('members', { signal }), 'members')
}

export const decideJoin = (id: string, action: 'approve' | 'deny' | 'revoke') =>
  adminCall(`joins/${encodeURIComponent(id)}/${action}`, { method: 'POST' })
