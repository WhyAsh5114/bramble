// Checks that the demo can run, without starting or changing anything.
// Prints env variable names, never values. Exit code 1 if any check fails.
//
// Usage: pnpm demo:preflight [--role relays,gateway,worker,admin] [--wait <seconds>]
// Without --role, the roles come from the env files under /etc/bramble.
// Other machines are reached with DEMO_GATEWAY_SSH / DEMO_WORKER_SSH (see README).
import { ambientSecrets, buildContext, detectRoles } from './context'
import { parseFlags } from './cli-args'
import { errorText } from './redact'
import { exitCode, formatReport, buildChecks, runPreflight } from './preflight'
import { parseRoles } from './topology'

async function main() {
  const { values } = parseFlags(process.argv.slice(2), { value: ['role', 'wait'], bool: [] })
  const detected = detectRoles()
  const roleArg = values.get('role')
  const localRoles = roleArg !== undefined ? parseRoles(roleArg) : detected.roles
  if (localRoles.length === 0) {
    throw new Error('no roles for this machine: pass --role, or run the installer so /etc/bramble holds its env files')
  }
  const waitSeconds = Number(values.get('wait') ?? '0')
  if (!Number.isFinite(waitSeconds) || waitSeconds < 0) throw new Error('--wait takes a number of seconds')

  const ctx = buildContext({ processEnv: process.env, localRoles, relayInstances: detected.relayInstances })
  console.log(`preflight for: ${localRoles.join(', ')}\n`)
  const results = await runPreflight(buildChecks(ctx), {
    waitMs: waitSeconds * 1000,
    intervalMs: 5000,
    onRetry: (ids, remainingMs) =>
      console.log(`waiting on ${ids.join(', ')} (${Math.ceil(remainingMs / 1000)} s left)`),
  })
  console.log(formatReport(results))
  process.exit(exitCode(results))
}

main().catch((err) => {
  console.error('preflight failed:', errorText(err, ambientSecrets(process.env)))
  process.exit(2)
})
