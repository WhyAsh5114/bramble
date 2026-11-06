import { describe, expect, it } from 'bun:test'
import path from 'node:path'
import { run } from './run-command'

describe('run', () => {
  it('returns the exit code and stdout', async () => {
    expect(await run('sh', ['-c', 'echo hello; exit 3'])).toEqual({ code: 3, stdout: 'hello\n' })
  })

  it('does not forward child stderr to the launcher stderr', () => {
    const here = path.join(import.meta.dir, 'run-command')
    const script = `import { run } from ${JSON.stringify(here)}; await run('sh', ['-c', 'echo SYNTHETIC_LEAK >&2; exit 1'])`
    const result = Bun.spawnSync([process.execPath, '-e', script])
    expect(result.exitCode).toBe(0)
    expect(result.stderr.toString()).not.toContain('SYNTHETIC_LEAK')
    expect(result.stdout.toString()).not.toContain('SYNTHETIC_LEAK')
  })
})
