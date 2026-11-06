// Returns the demo to its initial state on the real chain: the agent stays
// enrolled, is not revoked, and has no ACL. Run it on the admin machine.
//
// Usage: pnpm demo:reset [--agent <label>] [--gateway <label>] [--granter <label>]
//                        [--services web,db] [--dry-run]
// Fallbacks: DEMO_AGENT_LABEL, BRAMBLE_GATEWAY_LABEL, DEMO_GRANTER_LABEL, BRAMBLE_SERVICE_NAME.
// Signing uses SEPOLIA_PRIVATE_KEY. The granter key comes from BRAMBLE_GRANTER_PRIVATE_KEY
// when set, otherwise from the Key Ring file. No key is printed.
import { parseServiceNames, type LoadGranterKey } from '../../../admincli/src/lib'
import { cliDeps, loadGranterKeyFromRing } from '../../../admincli/src/lib-viem'
import { ambientSecrets } from './context'
import { parseFlags } from './cli-args'
import { errorText } from './redact'
import { resetDemo } from './reset'

function required(flag: string, value: string | undefined, envName: string): string {
  if (!value) throw new Error(`pass --${flag} or set ${envName}`)
  return value
}

async function main() {
  const { values, bools } = parseFlags(process.argv.slice(2), {
    value: ['agent', 'gateway', 'granter', 'services'],
    bool: ['dry-run'],
  })
  const env = process.env
  const loadGranterKey: LoadGranterKey = env.BRAMBLE_GRANTER_PRIVATE_KEY
    ? async () => env.BRAMBLE_GRANTER_PRIVATE_KEY as string
    : loadGranterKeyFromRing

  const result = await resetDemo(cliDeps(), {
    agentLabel: required('agent', values.get('agent') ?? env.DEMO_AGENT_LABEL, 'DEMO_AGENT_LABEL'),
    gatewayLabel: required('gateway', values.get('gateway') ?? env.BRAMBLE_GATEWAY_LABEL, 'BRAMBLE_GATEWAY_LABEL'),
    granterLabel: required('granter', values.get('granter') ?? env.DEMO_GRANTER_LABEL, 'DEMO_GRANTER_LABEL'),
    serviceNames: parseServiceNames(values.get('services') ?? env.BRAMBLE_SERVICE_NAME ?? ''),
    loadGranterKey,
    dryRun: bools.has('dry-run'),
  })
  console.log(
    `\nreset ${bools.has('dry-run') ? '(dry run) ' : ''}done for ${result.deviceFullname}: ` +
      (result.actions.length === 0 ? 'already in the initial state' : result.actions.join('; '))
  )
}

main().catch((err) => {
  console.error('\nreset failed:', errorText(err, ambientSecrets(process.env)))
  process.exit(1)
})
