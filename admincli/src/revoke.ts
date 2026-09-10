// Revokes (or un-revokes) a device via the EAC-gated `revoked` text record
// (docs/adr/0004) — distinct from scripts/provision-dev-tailnet/set-pubkey.ts's
// clear-pubkey revocation path, which stays in place unchanged for Gate
// 1.3's already-verified runbook. This is the role-scoped path: an account
// holding ROLE_SET_TEXT scoped to "revoked" (not "pubkey") can revoke
// without ever being able to rotate a device's key.
//
// Usage: bun run revoke.ts <device-label> <true|false>
//
// Signs through the Ledger (Gate 3.1, docs/05_BUILD_PLAN.md): no plaintext
// key, no software wallet client. `wallet-cli send` blocks on a physical
// button press; a disconnected/rejecting device makes this fail, not fall
// back to signing another way.
import { encodeFunctionData, toHex } from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import { tailnetName } from '../../sidecar/src/ens/config'
import { publicClient } from './setup'
import { resolverWriteAbi } from './roles'
import { sendViaDevice } from './ledger-send'

async function main() {
  const [deviceLabel, value] = process.argv.slice(2)
  if (!deviceLabel || (value !== 'true' && value !== 'false')) {
    throw new Error('usage: bun run revoke.ts <device-label> <true|false>')
  }

  const fullname = normalize(`${deviceLabel}.${tailnetName()}`)
  const resolverAddress = await publicClient.getEnsResolver({ name: fullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${fullname}`)
  const dnsName = toHex(packetToBytes(fullname))

  console.log(`setting revoked=${value} for ${fullname}...`)
  console.log('   confirm on the Ledger device...')
  const startedAt = Date.now()
  const data = encodeFunctionData({
    abi: resolverWriteAbi,
    functionName: 'setText',
    args: [dnsName, 'revoked', value],
  })
  const hash = await sendViaDevice(resolverAddress, data)
  console.log(`   tx: ${hash}`)
  const receipt = await publicClient.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') throw new Error('setText reverted on-chain')
  console.log(`   confirmed ${Date.now() - startedAt}ms after submit`)

  const after = await publicClient.getEnsText({ name: fullname, key: 'revoked' })
  if (after !== value) throw new Error(`revoked did not round-trip: got ${JSON.stringify(after)}`)
  console.log(`done. ${fullname} revoked=${after}.`)
}

main().catch((err) => {
  console.error('\nrevoke failed:', err)
  process.exit(1)
})
