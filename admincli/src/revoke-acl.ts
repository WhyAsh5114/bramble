// Revokes a specific ACL capability previously granted by set-acl.ts —
// removes the matching digest(s) from a device's `acl` text record
// (docs/adr/0005-acl-record-schema.md) instead of appending to it.
//
// Before this script existed there was no way to revoke a single grant at
// all: set-acl.ts only ever unions into the acl list. Revoking the granter
// identity itself (see the enroll/revoke flow, docs/adr/0004) stops that
// granter's *future* vouches and — as of Sept 11 2026 — its *past* ones too
// (gateway/acl.go's CheckACL now also checks !granter.Revoked), but neither
// touches a single already-published digest for an otherwise-still-trusted
// granter. This script is that missing removal path.
//
// Computes the exact same digest(s) set-acl.ts would have computed for the
// same (device, gateway, granter, services) inputs — see acl-grant.ts's
// computeGrantDigests, shared by both scripts so the two can never drift —
// and filters them out of the existing acl list rather than adding to it.
//
// Usage: bun run revoke-acl.ts <device-label> <gateway-label> <granter-label> <comma,separated,service,names>
import { createWalletClient, http, toHex } from 'viem'
import { packetToBytes } from 'viem/ens'
import { RPC_URL } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { resolverWriteAbi } from './roles'
import { computeGrantDigests } from './acl-grant'

async function main() {
  const [deviceLabel, gatewayLabel, granterLabel, servicesArg] = process.argv.slice(2)
  if (!deviceLabel || !gatewayLabel || !granterLabel || servicesArg === undefined) {
    throw new Error(
      'usage: bun run revoke-acl.ts <device-label> <gateway-label> <granter-label> <comma,separated,service,names>'
    )
  }
  const serviceNames = servicesArg
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)

  const {
    granterFullname,
    gatewayFullname,
    deviceFullname: fullname,
    digests: digestsToRemove,
  } = await computeGrantDigests(deviceLabel, gatewayLabel, granterLabel, serviceNames)
  console.log(`granter: ${granterFullname}`)
  console.log(`gateway: ${gatewayFullname}`)
  console.log(`device: ${fullname}`)
  console.log(
    `services: ${serviceNames.join(', ') || '(none)'} -> digests to remove: ${digestsToRemove.join(',') || '(empty)'}`
  )

  const account = accountFromEnv()
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  const resolverAddress = await publicClient.getEnsResolver({ name: fullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${fullname}`)
  const dnsName = toHex(packetToBytes(fullname))

  const before = await publicClient.getEnsText({ name: fullname, key: 'acl' })
  const existingDigests = (before ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
  console.log(`current acl digests for ${fullname}: ${before || '(unset)'}`)

  const toRemove = new Set(digestsToRemove)
  const remaining = existingDigests.filter((d) => !toRemove.has(d))
  if (remaining.length === existingDigests.length) {
    console.log(`none of the computed digests were present in ${fullname}'s acl — nothing to do.`)
    return
  }
  const value = remaining.join(',')

  const hash = await wallet.writeContract({
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'setText',
    args: [dnsName, 'acl', value],
  })
  console.log(`   tx: ${hash}`)
  const receipt = await publicClient.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') throw new Error('setText reverted on-chain')

  const after = await publicClient.getEnsText({ name: fullname, key: 'acl' })
  if (after !== value) throw new Error(`acl did not round-trip: got ${JSON.stringify(after)}`)
  console.log(`done. ${fullname}'s acl digests are now ${after === '' ? '(empty)' : after}.`)
}

main().catch((err) => {
  console.error('\nrevoke-acl failed:', err)
  process.exit(1)
})
