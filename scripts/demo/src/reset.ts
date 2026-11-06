// Returns the demo to its initial state: the agent device is enrolled, is not
// revoked, and has an empty `acl` record. It uses admincli's importable lib
// (ETH-28), so the same code paths that grant and revoke in the demo run here.
//
// It does not enrol a device: enrollment is a separate, software-signed step,
// so a missing `pubkey` is an error.
import { normalize } from 'viem/ens'
import {
  parseAclRecord,
  revokeAcl,
  setDeviceRevoked,
  type LibDeps,
  type LoadGranterKey,
} from '../../../admincli/src/lib'

export interface ResetParams {
  /** ENS label of the agent's device, for example the worker node. */
  agentLabel: string
  gatewayLabel: string
  granterLabel: string
  /** Services the demo grants. Their digests are removed first. */
  serviceNames: string[]
  loadGranterKey: LoadGranterKey
  /** Read the state and report the actions without sending a transaction. */
  dryRun?: boolean
}

export interface ResetResult {
  deviceFullname: string
  /** One line per action taken, or that would be taken when `dryRun` is set. */
  actions: string[]
  txHashes: string[]
}

const noop = () => {}

export async function resetDemo(deps: LibDeps, params: ResetParams): Promise<ResetResult> {
  const log = deps.log ?? noop
  const deviceFullname = normalize(`${params.agentLabel}.${deps.tailnetName}`)
  const actions: string[] = []
  const txHashes: string[] = []
  const dryRun = params.dryRun === true
  const verb = dryRun ? 'would' : 'did'

  const pubkey = await deps.resolver.getText(deviceFullname, 'pubkey')
  if (!pubkey) {
    throw new Error(
      `${deviceFullname} has no pubkey: it is not enrolled. Enroll the agent (admincli enroll) before a reset.`
    )
  }
  log(`agent: ${deviceFullname} (enrolled)`)

  const revoked = (await deps.resolver.getText(deviceFullname, 'revoked')) ?? ''
  if (revoked === 'true') {
    actions.push(`${verb} set revoked=false`)
    log(`${deviceFullname} is revoked; clearing`)
    if (!dryRun) {
      const result = await setDeviceRevoked(deps, { deviceLabel: params.agentLabel, value: false })
      txHashes.push(result.txHash)
    }
  } else {
    log(`${deviceFullname} is not revoked`)
  }

  // Test the raw record, not the parsed digests: a record such as ", ," holds no digest but is not empty.
  const before = (await deps.resolver.getText(deviceFullname, 'acl')) ?? ''
  const digests = parseAclRecord(before)
  if (before === '') {
    log(`${deviceFullname} has an empty acl record`)
  } else {
    actions.push(
      digests.length > 0
        ? `${verb} remove the digests for ${params.serviceNames.join(', ') || '(no services)'}`
        : `${verb} clear the acl record, which holds no digest`
    )
    let remaining = before
    if (!dryRun && digests.length > 0 && params.serviceNames.length > 0) {
      const change = await revokeAcl(deps, {
        deviceLabel: params.agentLabel,
        gatewayLabel: params.gatewayLabel,
        granterLabel: params.granterLabel,
        serviceNames: params.serviceNames,
        loadGranterKey: params.loadGranterKey,
      })
      if (change.txHash) txHashes.push(change.txHash)
      remaining = change.after
    }
    if (dryRun || remaining !== '') {
      if (digests.length > 0) actions.push(`${verb} clear any remaining acl record`)
      if (!dryRun) {
        log(`the acl record on ${deviceFullname} is not empty after the demo's revoke; clearing it`)
        const pending = await deps.signer.setText(deviceFullname, 'acl', '')
        log(`   tx: ${pending.hash}`)
        await pending.wait()
        txHashes.push(pending.hash)
      }
    }
  }

  if (!dryRun) {
    const finalRevoked = (await deps.resolver.getText(deviceFullname, 'revoked')) ?? ''
    const finalAcl = (await deps.resolver.getText(deviceFullname, 'acl')) ?? ''
    if (finalRevoked === 'true' || finalAcl !== '') {
      throw new Error(
        `reset did not reach the initial state for ${deviceFullname} (revoked=${finalRevoked || 'unset'}, acl ${finalAcl === '' ? 'empty' : 'not empty'})`
      )
    }
  }
  if (actions.length === 0) log('already in the initial state; nothing to do')
  return { deviceFullname, actions, txHashes }
}
