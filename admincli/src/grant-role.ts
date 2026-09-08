// Delegates one of Gate 2.1's three distinct EAC roles to another account.
// Usage:
//   bun run grant-role.ts enroll <account-address>
//     Grants ROLE_REGISTRAR (root-scoped, on the tailnet's own subregistry —
//     see docs/adr/0004) to <account-address>, letting it call register()
//     to create new device subnames on this tailnet.
//   bun run grant-role.ts rotate <device-label> <account-address>
//   bun run grant-role.ts revoke <device-label> <account-address>
//   bun run grant-role.ts acl <device-label> <account-address>
//   bun run grant-role.ts acl-granters <gateway-label> <account-address>
//     Grants ROLE_SET_TEXT scoped to the pubkey/revoked/acl/acl-granters
//     setter (respectively) on <device-label>'s own resolver to
//     <account-address>. acl-granters is the *gateway's own* trust
//     decision — see docs/adr/0005-acl-record-schema.md.
//
// The caller (SEPOLIA_PRIVATE_KEY) must already hold the corresponding admin
// role — ROLE_REGISTRAR_ADMIN for "enroll", or ALL_ROLES/ROLE_SET_TEXT_ADMIN
// on the device's resolver for "rotate"/"revoke" (true for whoever deployed
// that resolver, e.g. via enroll.ts).
import { createWalletClient, http, getAddress } from 'viem'
import { normalize } from 'viem/ens'
import { RPC_URL, tailnetName, tailnetRegistry, ROLE_REGISTRAR } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { registryWriteAbi, resolverWriteAbi, pubkeySetter, revokedSetter, aclSetter, aclGrantersSetter } from './roles'

const RESOLVER_SCOPED_ROLES = {
  rotate: { key: 'pubkey', setter: pubkeySetter },
  revoke: { key: 'revoked', setter: revokedSetter },
  acl: { key: 'acl', setter: aclSetter },
  'acl-granters': { key: 'acl-granters', setter: aclGrantersSetter },
} as const

async function main() {
  const [role, ...rest] = process.argv.slice(2)
  const account = accountFromEnv()
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  async function writeAndWait(label: string, args: Parameters<typeof wallet.writeContract>[0]) {
    const hash = await wallet.writeContract(args)
    console.log(`   tx (${label}): ${hash}`)
    const receipt = await publicClient.waitForTransactionReceipt({ hash })
    if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
    return receipt
  }

  if (role === 'enroll') {
    const [grantee] = rest
    if (!grantee) throw new Error('usage: bun run grant-role.ts enroll <account-address>')
    console.log(`Granting ROLE_REGISTRAR on ${tailnetRegistry()} to ${grantee}...`)
    await writeAndWait('grantRootRoles(ROLE_REGISTRAR)', {
      address: tailnetRegistry(),
      abi: registryWriteAbi,
      functionName: 'grantRootRoles',
      args: [ROLE_REGISTRAR, getAddress(grantee)],
    })
    console.log('done — that account can now call register() on this tailnet.')
    return
  }

  if (role in RESOLVER_SCOPED_ROLES) {
    const { key, setter } = RESOLVER_SCOPED_ROLES[role as keyof typeof RESOLVER_SCOPED_ROLES]
    const [deviceLabel, grantee] = rest
    if (!deviceLabel || !grantee) {
      throw new Error(`usage: bun run grant-role.ts ${role} <device-label> <account-address>`)
    }
    const fullname = normalize(`${deviceLabel}.${tailnetName()}`)
    const resolverAddress = await publicClient.getEnsResolver({ name: fullname })
    if (!resolverAddress) throw new Error(`no resolver found for ${fullname}`)

    console.log(`Granting ROLE_SET_TEXT(${key}) on ${fullname}'s resolver to ${grantee}...`)
    await writeAndWait(`grantSetterRoles(${role})`, {
      address: resolverAddress,
      abi: resolverWriteAbi,
      functionName: 'grantSetterRoles',
      args: [setter(), getAddress(grantee)],
    })
    console.log(`done — that account now controls ${key} on ${deviceLabel}.`)
    return
  }

  throw new Error(`usage: bun run grant-role.ts <enroll|rotate|revoke|acl|acl-granters> ...`)
}

main().catch((err) => {
  console.error('\ngrant-role failed:', err)
  process.exit(1)
})
