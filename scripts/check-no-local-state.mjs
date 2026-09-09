#!/usr/bin/env node
// Gate 2.2 (docs/05_BUILD_PLAN.md): "Nothing hard-coded in the demo path."
// The gate's own test is "delete every local cache, restart both nodes,
// confirm the mesh reassembles purely from chain state" — and the strongest
// form of that property is structural: the demo runtime path must contain
// *no local-state writes at all*, so there is never a cache to delete. This
// check is that grep, automated and wired into `pnpm verify` so a future
// write path can't silently reintroduce one.
//
// Scope, deliberately narrow: only the demo *runtime* path is scanned —
// `brambled/**/*.go` (excluding `*_test.go`, which legitimately write
// fixtures) and `sidecar/src/**/*.ts`. Admin tooling (`admincli`,
// `scripts/provision-dev-tailnet`) writes local files legitimately and is
// out of scope; the gate covers what a node actually runs, not what an
// operator runs to set it up. Reads (`os.ReadFile`) are not flagged — a
// config *file* is a different smell than a state *write* — but note that
// no reads exist in the demo path today either: brambled and the sidecar
// take everything from flags/env/chain (see brambled/README.md's Gate 2.2
// runbook for the full state inventory).
//
// No dependencies, not a workspace package — same "small standalone script"
// treatment as scripts/check-no-endpoints.mjs.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const SCAN_DIRS = [
  { dir: 'brambled', extensions: new Set(['.go']), exclude: (rel) => rel.endsWith('_test.go') },
  { dir: 'sidecar/src', extensions: new Set(['.ts']), exclude: () => false },
]
const EXCLUDE_SEGMENTS = new Set(['node_modules', '.git', 'dist'])

// Go: any filesystem mutation through the os/ioutil packages. Call-paren
// form, so os.Stderr/os.Getenv and friends can't false-positive.
const GO_WRITE_CALL =
  /\bos\.(WriteFile|Create|OpenFile|MkdirAll|Mkdir|CreateTemp|MkdirTemp|Rename|Remove|RemoveAll)\s*\(|\bioutil\.WriteFile\s*\(/g
// TS: fs module write helpers (directly or via an `fs.` prefix), plus
// Bun.write — the Bun runtime's own write API, the most likely way a
// sidecar would persist something. Call-paren form, so a bare `import {
// writeFileSync } from 'node:fs'` doesn't false-positive.
const TS_WRITE_CALL =
  /\b(?:fs\.)?(writeFileSync|writeFile|appendFileSync|appendFile|mkdirSync|mkdir|createWriteStream)\s*\(|\bBun\.write\s*\(/g

function isExcludedSegments(relPath) {
  return relPath.split(path.sep).some((s) => EXCLUDE_SEGMENTS.has(s))
}

function walk(dir, out) {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry)
    const rel = path.relative(repoRoot, full)
    if (isExcludedSegments(rel)) continue
    const stat = statSync(full)
    if (stat.isDirectory()) {
      walk(full, out)
    } else {
      out.push(full)
    }
  }
}

const findings = []
for (const { dir, extensions, exclude } of SCAN_DIRS) {
  const full = path.join(repoRoot, dir)
  const files = []
  try {
    walk(full, files)
  } catch {
    // dir doesn't exist — fine, nothing to scan there
  }
  for (const file of files) {
    const rel = path.relative(repoRoot, file)
    if (!extensions.has(path.extname(file)) || exclude(rel)) continue
    const content = readFileSync(file, 'utf-8')
    const re = extensions.has('.go') ? GO_WRITE_CALL : TS_WRITE_CALL
    for (const match of content.matchAll(re)) {
      const line = content.slice(0, match.index).split('\n').length
      findings.push(`${rel}:${line}: local-state write ${JSON.stringify(match[0].replace(/\s*\($/, '()'))}`)
    }
  }
}

if (findings.length > 0) {
  console.error('Gate 2.2 violation: the demo path writes local state.\n')
  for (const f of findings) console.error('  ' + f)
  console.error(
    '\nA node must reassemble purely from chain state — no local caches, no persisted config (see docs/05_BUILD_PLAN.md Gate 2.2).'
  )
  process.exit(1)
}

const scanned = SCAN_DIRS.map(({ dir }) => dir).join(', ')
console.log(
  `check-no-local-state: scanned ${scanned} (excluding *_test.go and admin tooling) — no local-state writes in the demo path. (Gate 2.2)`
)
