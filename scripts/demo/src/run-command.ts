import { spawn } from 'node:child_process'
import type { RunCommand } from './launch'

// Child stderr is read and dropped, never forwarded: a failing command can echo
// an environment value, and redacting afterwards cannot recall bytes already written.
// The failure message points at journalctl instead.
export const run: RunCommand = (file, args) =>
  new Promise((resolve, reject) => {
    const child = spawn(file, args, { stdio: ['inherit', 'pipe', 'pipe'] })
    let stdout = ''
    child.stdout.on('data', (chunk: Buffer) => {
      stdout += chunk.toString()
    })
    child.stderr.resume()
    child.on('error', reject)
    child.on('close', (code) => resolve({ code: code ?? 1, stdout }))
  })
