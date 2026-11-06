// Reads KEY=VALUE files (the repo `.env` and /etc/bramble/*.env) into plain
// objects. Values stay in memory and are only tested for presence.
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import type { Env } from './preflight'

/** Parses dotenv-style text: `KEY=value`, optional `export `, `#` comments, one layer of matching quotes. */
export function parseEnvText(text: string): Env {
  const env: Env = {}
  for (const line of text.split('\n')) {
    const match = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$/.exec(line)
    if (!match) continue
    let value = match[2].trim()
    const quote = value[0]
    if ((quote === '"' || quote === "'") && value.length >= 2 && value.endsWith(quote)) value = value.slice(1, -1)
    else value = value.replace(/\s+#.*$/, '')
    env[match[1]] = value
  }
  return env
}

/** Returns the parsed file, or undefined when it is missing or unreadable (for example root-only). */
export function readEnvFile(path: string): Env | undefined {
  try {
    if (!existsSync(path)) return undefined
    return parseEnvText(readFileSync(path, 'utf8'))
  } catch {
    return undefined
  }
}

export function listDir(path: string): string[] {
  try {
    return readdirSync(path)
  } catch {
    return []
  }
}
