// Sets a gateway's own `acl-granters` record (docs/adr/0005) — the list of
// already-enrolled identity labels this gateway will accept ACL vouches
// from. This is the gateway owner's own trust decision, independent of any
// tailnet-wide admin: EAC-gated by grantSetterRoles on the gateway's own
// resolver (roles.ts's aclGrantersSetter()), so only whoever owns/deployed
// this specific gateway's resolver — or someone they've delegated to —
// controls whose digests it will honor.
//
// Usage: bun run set-acl-granters.ts <gateway-label> <comma,separated,granter,labels>
import { createWalletClient, http, toHex } from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import { RPC_URL, tailnetName } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { resolverWriteAbi } from './roles'

async function main() {
  const [gatewayLabel, granterLabelsArg] = process.argv.slice(2)
  if (!gatewayLabel || granterLabelsArg === undefined) {
    throw new Error('usage: bun run set-acl-granters.ts <gateway-label> <comma,separated,granter,labels>')
  }
  const value = granterLabelsArg
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
    .join(',')

  const account = accountFromEnv()
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  const fullname = normalize(`${gatewayLabel}.${tailnetName()}`)
  const resolverAddress = await publicClient.getEnsResolver({ name: fullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${fullname}`)
  const dnsName = toHex(packetToBytes(fullname))

  const before = await publicClient.getEnsText({ name: fullname, key: 'acl-granters' })
  console.log(`current acl-granters for ${fullname}: ${before || '(unset)'}`)

  const hash = await wallet.writeContract({
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'setText',
    args: [dnsName, 'acl-granters', value],
  })
  console.log(`   tx: ${hash}`)
  const receipt = await publicClient.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') throw new Error('setText reverted on-chain')

  const after = await publicClient.getEnsText({ name: fullname, key: 'acl-granters' })
  if (after !== value) throw new Error(`acl-granters did not round-trip: got ${JSON.stringify(after)}`)
  console.log(`done. ${fullname}'s acl-granters is now ${after === '' ? '(empty)' : after}.`)
}

main().catch((err) => {
  console.error('\nset-acl-granters failed:', err)
  process.exit(1)
})
