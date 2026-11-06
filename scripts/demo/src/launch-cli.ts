// Starts what this machine needs for the demo and prints what to start on the
// others. Run it on each machine; the roles come from the env files the
// installer seeded under /etc/bramble, or from --role.
//
// Usage: pnpm demo [--role relays,gateway,worker,admin] [--dry-run] [--no-preflight] [--wait <seconds>]
// DEMO_NAT_BOX=1 starts bramble-nat-box first and probes local nodes inside it.
import { ambientSecrets, buildContext, detectRoles, loadEnv, natBoxEnabled, REPO_ENV_PATH } from './context'
import { parseFlags } from './cli-args'
import { readEnvFile } from './env-file'
import { formatPlan, planLaunch, startUnits } from './launch'
import { run } from './run-command'
import { buildChecks, exitCode, formatReport, runPreflight } from './preflight'
import { errorText } from './redact'
import { parseRelayInstances, parseRoles } from './topology'

async function main() {
  const { values, bools } = parseFlags(process.argv.slice(2), {
    value: ['role', 'wait'],
    bool: ['dry-run', 'no-preflight'],
  })
  const env = loadEnv(process.env, readEnvFile(REPO_ENV_PATH))
  const detected = detectRoles()
  const roleArg = values.get('role')
  const localRoles = roleArg !== undefined ? parseRoles(roleArg) : detected.roles
  if (localRoles.length === 0) {
    throw new Error('no roles for this machine: pass --role, or run the installer so /etc/bramble holds its env files')
  }
  const relayInstances =
    env.DEMO_RELAY_INSTANCES !== undefined ? parseRelayInstances(env.DEMO_RELAY_INSTANCES) : detected.relayInstances
  const plan = planLaunch(localRoles, {
    relayInstances: relayInstances.length > 0 ? relayInstances : parseRelayInstances(undefined),
    natBox: natBoxEnabled(env),
  })
  console.log(formatPlan(plan), '\n')
  if (bools.has('dry-run')) {
    console.log('dry run: nothing started')
    return
  }

  const result = await startUnits(plan, run, {
    asRoot: process.getuid?.() === 0,
    log: (line) => console.log(line),
  })
  if (result.failed) {
    console.error(`\nstopped: ${result.failed.unit}: ${result.failed.reason}`)
    process.exit(1)
  }
  console.log(`\nstarted: ${result.started.join(', ')}`)
  if (bools.has('no-preflight')) return

  // Tor takes about a minute to bootstrap, so the default wait covers it.
  const waitSeconds = Number(values.get('wait') ?? '120')
  if (!Number.isFinite(waitSeconds) || waitSeconds < 0) throw new Error('--wait takes a number of seconds')
  console.log('\nrunning preflight\n')
  const ctx = buildContext({ processEnv: process.env, localRoles, relayInstances: detected.relayInstances })
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
  console.error('demo launch failed:', errorText(err, ambientSecrets(process.env)))
  process.exit(2)
})
