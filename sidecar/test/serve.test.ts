import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const fixture = fileURLToPath(new URL('./fixtures/slow-bun-server.ts', import.meta.url))

function startBunServer(delayMs: number) {
  return new Promise<{ port: number; stop: () => void }>((resolve, reject) => {
    const child = spawn('bun', [fixture, String(delayMs)], { stdio: ['ignore', 'pipe', 'inherit'] })
    child.once('error', reject)
    child.stdout.once('data', (chunk: Buffer) =>
      resolve({ port: Number(chunk.toString().trim()), stop: () => child.kill() })
    )
  })
}

describe('sidecar server request timeout', () => {
  // Bun's default closes a pending response after 10 s; Tor-backed calls take longer.
  it('answers a request delayed past Bun’s 10 s default', async () => {
    const server = await startBunServer(15_000)
    try {
      const res = await fetch(`http://127.0.0.1:${server.port}/`, { signal: AbortSignal.timeout(30_000) })
      expect(res.status).toBe(200)
      expect(await res.text()).toBe('ok')
    } finally {
      server.stop()
    }
  }, 40_000)
})
