// How the preflight reaches a node's loopback services. The sidecar (7890) and
// the brambled status API (7899) bind to 127.0.0.1, so another machine's node is
// reached with ssh + curl, and a node inside the NAT box with `ip netns exec`.
// Commands run through execFile with no local shell. Anything that reaches the
// remote shell is checked against a strict pattern first.
import { execFile } from 'node:child_process'
import { connect } from 'node:net'
import type { HttpResponse, NodeProbe } from './preflight'

export interface ProbeSpec {
  /** An ssh target (`user@host` or a Host alias). Unset means this machine. */
  ssh?: string
  /** A network namespace to run in, for a node inside the NAT box. */
  netns?: string
}

export interface ExecResult {
  stdout: string
}

export type Exec = (file: string, args: string[], timeoutMs: number) => Promise<ExecResult>

const SSH_TARGET = /^[A-Za-z0-9][A-Za-z0-9._@-]*$/
const NETNS = /^[A-Za-z0-9_-]+$/
const URL_PATH = /^\/[A-Za-z0-9/_-]*$/

export const CURL_TIMEOUT_SECONDS = 5

function shellQuote(word: string): string {
  return /^[A-Za-z0-9_@%+=:,./-]+$/.test(word) ? word : `'${word.replace(/'/g, `'\\''`)}'`
}

/** The argv that fetches one loopback URL on the target, printing the body then the status code. */
export function buildProbeCommand(spec: ProbeSpec, port: number, path: string): { file: string; args: string[] } {
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error(`invalid port ${port}`)
  if (!URL_PATH.test(path)) throw new Error(`invalid probe path "${path}"`)
  if (spec.ssh !== undefined && !SSH_TARGET.test(spec.ssh)) throw new Error('invalid ssh target')
  if (spec.netns !== undefined && !NETNS.test(spec.netns)) throw new Error('invalid network namespace name')

  const curl = [
    'curl',
    '-sS',
    '-m',
    String(CURL_TIMEOUT_SECONDS),
    '-w',
    '\\n%{http_code}',
    `http://127.0.0.1:${port}${path}`,
  ]
  const inner = spec.netns ? ['sudo', '-n', 'ip', 'netns', 'exec', spec.netns, ...curl] : curl
  if (spec.ssh === undefined) return { file: inner[0], args: inner.slice(1) }
  return {
    file: 'ssh',
    args: [
      '-o',
      'BatchMode=yes',
      '-o',
      'ConnectTimeout=5',
      '-o',
      'StrictHostKeyChecking=yes',
      '--',
      spec.ssh,
      inner.map(shellQuote).join(' '),
    ],
  }
}

function parseJsonOrNull(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

/** Parses `curl -w '\n%{http_code}'` output into a status and a JSON body (null when the body is not JSON). */
export function parseCurlOutput(stdout: string): HttpResponse {
  const cut = stdout.lastIndexOf('\n')
  const status = Number(stdout.slice(cut + 1).trim())
  if (cut < 0 || !Number.isInteger(status) || status < 100) throw new Error('curl printed no HTTP status')
  return { status, body: parseJsonOrNull(stdout.slice(0, cut)) }
}

export const execFileExec: Exec = (file, args, timeoutMs) =>
  new Promise((resolve, reject) => {
    execFile(file, args, { timeout: timeoutMs, maxBuffer: 1 << 20 }, (err, stdout, stderr) => {
      if (err) reject(new Error(`${file} failed: ${stderr.trim().split('\n')[0] || err.message}`))
      else resolve({ stdout })
    })
  })

export async function httpGet(url: string, timeoutMs: number): Promise<HttpResponse> {
  const res = await fetch(url, { signal: AbortSignal.timeout(timeoutMs) })
  return { status: res.status, body: parseJsonOrNull(await res.text()) }
}

/** Opens and closes a TCP connection to `host:port`. */
export function tcpConnect(hostPort: string, timeoutMs: number): Promise<void> {
  const cut = hostPort.lastIndexOf(':')
  const host = hostPort.slice(0, cut)
  const port = Number(hostPort.slice(cut + 1))
  return new Promise((resolve, reject) => {
    const socket = connect({ host, port })
    socket.setTimeout(timeoutMs)
    socket.once('connect', () => {
      socket.destroy()
      resolve()
    })
    socket.once('timeout', () => {
      socket.destroy()
      reject(new Error('timed out'))
    })
    socket.once('error', (err) => {
      socket.destroy()
      reject(err)
    })
  })
}

export function describeSpec(spec: ProbeSpec, role: string): string {
  const where = spec.ssh === undefined ? 'this machine' : `${role} via ssh`
  return spec.netns ? `${where}, NAT box` : where
}

export function makeNodeProbe(
  spec: ProbeSpec,
  role: string,
  exec: Exec = execFileExec,
  get: typeof httpGet = httpGet
): NodeProbe {
  const needsExec = spec.ssh !== undefined || spec.netns !== undefined
  return {
    describe: describeSpec(spec, role),
    async getJson(port, path) {
      if (!needsExec) return get(`http://127.0.0.1:${port}${path}`, CURL_TIMEOUT_SECONDS * 1000)
      const { file, args } = buildProbeCommand(spec, port, path)
      const { stdout } = await exec(file, args, (CURL_TIMEOUT_SECONDS + 10) * 1000)
      return parseCurlOutput(stdout)
    },
  }
}
