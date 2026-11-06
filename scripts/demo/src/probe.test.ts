import { describe, expect, it } from 'bun:test'
import { buildProbeCommand, makeNodeProbe, parseCurlOutput, type Exec } from './probe'

describe('buildProbeCommand', () => {
  it('runs curl directly on this machine', () => {
    const { file, args } = buildProbeCommand({}, 7890, '/health')
    expect(file).toBe('curl')
    expect(args.at(-1)).toBe('http://127.0.0.1:7890/health')
  })

  it('wraps the command in ssh with no password prompt and a pinned host key check', () => {
    const { file, args } = buildProbeCommand({ ssh: 'ubuntu@worker.example.org' }, 7899, '/endpoint')
    expect(file).toBe('ssh')
    expect(args).toContain('BatchMode=yes')
    expect(args.slice(-2)[0]).toBe('ubuntu@worker.example.org')
    expect(args.at(-1)).toBe("curl -sS -m 5 -w '\\n%{http_code}' http://127.0.0.1:7899/endpoint")
    expect(args[args.indexOf('--') + 1]).toBe('ubuntu@worker.example.org')
  })

  it('runs inside the NAT box namespace with sudo -n', () => {
    const { file, args } = buildProbeCommand({ netns: 'bramble-node' }, 7890, '/tor/status')
    expect(file).toBe('sudo')
    expect(args.slice(0, 5)).toEqual(['-n', 'ip', 'netns', 'exec', 'bramble-node'])
  })

  it('combines ssh and the namespace', () => {
    const { args } = buildProbeCommand({ ssh: 'worker', netns: 'bramble-node' }, 7890, '/health')
    expect(args.at(-1)).toContain('sudo -n ip netns exec bramble-node curl')
  })

  it('rejects values that could reach the remote shell or add an ssh option', () => {
    expect(() => buildProbeCommand({ ssh: '-oProxyCommand=evil' }, 7890, '/health')).toThrow()
    expect(() => buildProbeCommand({ ssh: 'host; rm -rf /' }, 7890, '/health')).toThrow()
    expect(() => buildProbeCommand({ netns: 'x; id' }, 7890, '/health')).toThrow()
    expect(() => buildProbeCommand({}, 7890, '/health; id')).toThrow()
    expect(() => buildProbeCommand({}, 7890, 'health')).toThrow()
    expect(() => buildProbeCommand({}, 0, '/health')).toThrow()
  })
})

describe('parseCurlOutput', () => {
  it('splits the body from the status code', () => {
    expect(parseCurlOutput('{"ok":true}\n200')).toEqual({ status: 200, body: { ok: true } })
  })

  it('keeps a non-JSON body as null', () => {
    expect(parseCurlOutput('not json\n503')).toEqual({ status: 503, body: null })
    expect(parseCurlOutput('\n404')).toEqual({ status: 404, body: null })
  })

  it('throws when curl printed no status', () => {
    expect(() => parseCurlOutput('')).toThrow()
    expect(() => parseCurlOutput('{"ok":true}')).toThrow()
  })
})

describe('makeNodeProbe', () => {
  it('uses the injected exec for ssh and parses its output', async () => {
    const seen: Array<{ file: string; args: string[] }> = []
    const exec: Exec = async (file, args) => {
      seen.push({ file, args })
      return { stdout: '{"state":"ready"}\n200' }
    }
    const probe = makeNodeProbe({ ssh: 'worker' }, 'worker', exec)
    expect(probe.describe).toBe('worker via ssh')
    expect(await probe.getJson(7890, '/tor/status')).toEqual({ status: 200, body: { state: 'ready' } })
    expect(seen[0].file).toBe('ssh')
  })

  it('uses plain HTTP for this machine without a namespace', async () => {
    const urls: string[] = []
    const probe = makeNodeProbe(
      {},
      'gateway',
      async () => ({ stdout: '' }),
      async (url) => {
        urls.push(url)
        return { status: 200, body: {} }
      }
    )
    await probe.getJson(7890, '/health')
    expect(urls).toEqual(['http://127.0.0.1:7890/health'])
  })

  it('propagates an exec failure so the check reports it', async () => {
    const probe = makeNodeProbe({ ssh: 'worker' }, 'worker', async () => {
      throw new Error('ssh failed: Permission denied')
    })
    await expect(probe.getJson(7890, '/health')).rejects.toThrow(/Permission denied/)
  })
})
