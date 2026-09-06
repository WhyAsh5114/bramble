#!/usr/bin/env node
// Gate 1.4 (docs/05_BUILD_PLAN.md): "No endpoints on device or agent
// subnames." The gate's own suggested test is "grep the codebase" — this is
// that grep, automated and wired into `pnpm verify` so a future write path
// can't silently violate it. Relay subnames are the one namespace allowed to
// carry endpoint data (docs/adr/0003-rendezvous-relay-split.md); nothing
// under `relay/` is scanned.
//
// Scope, deliberately narrow: this only inspects the literal arguments of
// setText() calls (the one ENS write mechanism this repo uses) — not every
// IP:port-shaped string anywhere in scanned files, which would false-positive
// on ordinary WireGuard test fixtures (brambled/*_test.go) that have nothing
// to do with ENS. A value passed via a variable (not a literal) can't be
// caught by grep — that's a known limit of this check, not a gap to silently
// paper over; the gate's own manual-read requirement covers what static
// analysis can't (see docs/05_BUILD_PLAN.md Gate 1.4).
//
// No dependencies, not a workspace package — same "small standalone script"
// treatment as scripts/gate0.3-eac-check, except this one runs on every push
// and is kept current, not frozen.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const SCAN_DIRS = ['sidecar/src', 'scripts/provision-dev-tailnet', 'brambled']
const EXCLUDE_SEGMENTS = new Set(['node_modules', '.git', 'dist'])
const EXCLUDE_PREFIXES = ['scripts/gate0.3-eac-check', 'relay']
const SCAN_EXTENSIONS = new Set(['.ts', '.go'])

const BANNED_KEYS = /^(endpoint|address|addr|ip|host|port|url)$/i
const IP_PORT_LITERAL = /\b\d{1,3}(?:\.\d{1,3}){3}:\d{2,5}\b/
// functionName: 'setText' ... args: [<name>, <key>, <value>] — the shape
// every ENS write in this repo uses (see register-device2.ts, index.ts).
const SET_TEXT_CALL = /functionName:\s*['"]setText['"][\s\S]{0,200}?args:\s*\[([^\]]*)\]/g

function isExcluded(relPath) {
  const segments = relPath.split(path.sep)
  if (segments.some((s) => EXCLUDE_SEGMENTS.has(s))) return true
  return EXCLUDE_PREFIXES.some((p) => relPath === p || relPath.startsWith(p + '/'))
}

function walk(dir, out) {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry)
    const rel = path.relative(repoRoot, full)
    if (isExcluded(rel)) continue
    const stat = statSync(full)
    if (stat.isDirectory()) {
      walk(full, out)
    } else if (SCAN_EXTENSIONS.has(path.extname(full))) {
      out.push(full)
    }
  }
}

function splitArgs(argsText) {
  // Simple top-level comma split — every setText() args array in this
  // codebase holds only identifiers/string literals, never nested brackets.
  return argsText.split(',').map((s) => s.trim())
}

function stripQuotes(s) {
  return s.replace(/^['"]|['"]$/g, '')
}

const findings = []
const files = []
for (const dir of SCAN_DIRS) {
  const full = path.join(repoRoot, dir)
  try {
    walk(full, files)
  } catch {
    // dir doesn't exist — fine, nothing to scan there
  }
}

for (const file of files) {
  const rel = path.relative(repoRoot, file)
  const content = readFileSync(file, 'utf-8')

  for (const match of content.matchAll(SET_TEXT_CALL)) {
    const line = content.slice(0, match.index).split('\n').length
    const args = splitArgs(match[1])
    const key = args[1] ? stripQuotes(args[1]) : ''
    if (BANNED_KEYS.test(key)) {
      findings.push(`${rel}:${line}: setText() writes banned key ${JSON.stringify(key)}`)
    }
    const ipMatch = match[1].match(IP_PORT_LITERAL)
    if (ipMatch) {
      findings.push(`${rel}:${line}: setText() call carries an IP:port-shaped literal ${JSON.stringify(ipMatch[0])}`)
    }
  }
}

if (findings.length > 0) {
  console.error('Gate 1.4 violation: endpoint data written toward a device/agent ENS record.\n')
  for (const f of findings) console.error('  ' + f)
  console.error('\nRelay subnames are the only namespace allowed to carry endpoint data (docs/adr/0003).')
  process.exit(1)
}

console.log(
  `check-no-endpoints: scanned ${files.length} file(s) across ${SCAN_DIRS.join(', ')} — no endpoint data found. (Gate 1.4)`
)
