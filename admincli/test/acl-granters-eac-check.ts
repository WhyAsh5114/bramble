// Section B verification: table-driven EAC permit/deny check for the
// `acl-granters` setter role (docs/adr/0005-acl-record-schema.md) — the
// gateway-owner-controlled half of the trust-store model, independent from
// `acl` above (a device's own capability list) and from pubkey/revoked.
// Mirrors gate2.1-eac-check.ts's and acl-eac-check.ts's technique exactly.
// Only an account holding ROLE_SET_TEXT scoped to the acl-granters setter
// on a resolver may write that resolver's `acl-granters` record —
// rotator/revoker/acl-setter (every other role this project has built) and
// a no-role account must all be denied.
//
// Run manually against Sepolia — costs real gas for the setup grants,
// depends on live testnet state, not wired into pnpm verify/CI.
//
// Usage: bun run test/acl-granters-eac-check.ts
// Requires SEPOLIA_PRIVATE_KEY in .env to hold ALL_ROLES on the fixture
// device's resolver (true for whoever ran scripts/provision-dev-tailnet).
import { createWalletClient, http, toHex } from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import { privateKeyToAccount, generatePrivateKey } from 'viem/accounts'
import fixture from '../../sidecar/test/fixtures/dev-tailnet.json'

process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry

import { RPC_URL } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from '../src/setup'
import { resolverWriteAbi, pubkeySetter, revokedSetter, aclSetter, aclGrantersSetter } from '../src/roles'

const results: Record<string, boolean> = {}
function report(name: string, pass: boolean) {
  results[name] = pass
  console.log(`${pass ? '✅ PASS' : '❌ FAIL'} — ${name}`)
}

async function main() {
  const admin = accountFromEnv()
  const wallet = createWalletClient({ account: admin, chain: hackathonSepolia, transport: http(RPC_URL) })

  async function writeAndWait(label: string, args: Parameters<typeof wallet.writeContract>[0]) {
    const hash = await wallet.writeContract(args)
    console.log(`   tx (${label}): ${hash}`)
    const receipt = await publicClient.waitForTransactionReceipt({ hash })
    if (receipt.status !== 'success') throw new Error(`${label} reverted on-chain`)
    return receipt
  }

  const rotator = privateKeyToAccount(generatePrivateKey())
  const revoker = privateKeyToAccount(generatePrivateKey())
  const aclSetterAcct = privateKeyToAccount(generatePrivateKey())
  const grantersSetterAcct = privateKeyToAccount(generatePrivateKey())
  const noRole = privateKeyToAccount(generatePrivateKey())
  console.log('Test accounts:')
  console.log(`  rotator:        ${rotator.address}`)
  console.log(`  revoker:        ${revoker.address}`)
  console.log(`  aclSetter:      ${aclSetterAcct.address}`)
  console.log(`  grantersSetter: ${grantersSetterAcct.address}`)
  console.log(`  noRole:         ${noRole.address}`)

  const gatewayFullname = normalize(fixture.deviceFullname)
  const resolverAddress = await publicClient.getEnsResolver({ name: gatewayFullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${gatewayFullname}`)
  const dnsName = toHex(packetToBytes(gatewayFullname))

  console.log('\n[setup 1/4] Granting ROLE_SET_TEXT(pubkey) to rotator...')
  await writeAndWait('grantSetterRoles(pubkey)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [pubkeySetter(), rotator.address],
  })

  console.log('\n[setup 2/4] Granting ROLE_SET_TEXT(revoked) to revoker...')
  await writeAndWait('grantSetterRoles(revoked)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [revokedSetter(), revoker.address],
  })

  console.log('\n[setup 3/4] Granting ROLE_SET_TEXT(acl) to aclSetter...')
  await writeAndWait('grantSetterRoles(acl)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [aclSetter(), aclSetterAcct.address],
  })

  console.log('\n[setup 4/4] Granting ROLE_SET_TEXT(acl-granters) to grantersSetter...')
  await writeAndWait('grantSetterRoles(acl-granters)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [aclGrantersSetter(), grantersSetterAcct.address],
  })

  const accounts = { rotator, revoker, aclSetter: aclSetterAcct, grantersSetter: grantersSetterAcct, noRole } as const

  console.log('\n================ TABLE ================')
  for (const [name, acct] of Object.entries(accounts)) {
    let ok: boolean
    try {
      await publicClient.simulateContract({
        account: acct.address,
        address: resolverAddress,
        abi: resolverWriteAbi,
        functionName: 'setText',
        args: [dnsName, 'acl-granters', 'acl-granters-eac-check-test-label'],
      })
      ok = true
    } catch {
      ok = false
    }
    report(`${name} -> set-acl-granters`, name === 'grantersSetter' ? ok : !ok)
  }

  console.log('\n================ ACL-GRANTERS ROLE SUMMARY ================')
  const allPassed = Object.values(results).every(Boolean)
  console.log(
    allPassed
      ? '✅ ALL CHECKS PASSED — acl-granters is governed by its own EAC role, independent from pubkey/revoked/acl.'
      : '❌ AT LEAST ONE CHECK FAILED — see above.'
  )
  process.exit(allPassed ? 0 : 1)
}

main().catch((err) => {
  console.error('\nacl-granters EAC check failed:', err)
  process.exit(1)
})
