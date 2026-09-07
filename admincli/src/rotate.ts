// Rotates a device's pubkey text record — the EAC-gated rotation path
// (docs/adr/0004). Functionally the non-empty-value case of
// scripts/provision-dev-tailnet/set-pubkey.ts, reimplemented here as the
// real admin-CLI command so it can be exercised under a role-scoped
// SEPOLIA_PRIVATE_KEY rather than only the tailnet's own deployer key.
//
// Usage: bun run rotate.ts <device-label> <new-pubkey>
import { createWalletClient, http, toHex } from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import { RPC_URL, tailnetName } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { resolverWriteAbi } from './roles'

async function main() {
  const [deviceLabel, newPubkey] = process.argv.slice(2)
  if (!deviceLabel || !newPubkey) {
    throw new Error('usage: bun run rotate.ts <device-label> <new-pubkey>')
  }

  const account = accountFromEnv()
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  const fullname = normalize(`${deviceLabel}.${tailnetName()}`)
  const resolverAddress = await publicClient.getEnsResolver({ name: fullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${fullname}`)
  const dnsName = toHex(packetToBytes(fullname))

  const before = await publicClient.getEnsText({ name: fullname, key: 'pubkey' })
  console.log(`current pubkey for ${fullname}: ${before ?? '(unset)'}`)

  const hash = await wallet.writeContract({
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'setText',
    args: [dnsName, 'pubkey', newPubkey],
  })
  console.log(`   tx: ${hash}`)
  const receipt = await publicClient.waitForTransactionReceipt({ hash })
  if (receipt.status !== 'success') throw new Error('setText reverted on-chain')

  const after = await publicClient.getEnsText({ name: fullname, key: 'pubkey' })
  if (after !== newPubkey) throw new Error(`pubkey did not round-trip: got ${JSON.stringify(after)}`)
  console.log(`done. ${fullname}'s pubkey is now ${after}.`)
}

main().catch((err) => {
  console.error('\nrotate failed:', err)
  process.exit(1)
})
