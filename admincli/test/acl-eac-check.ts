// Section B verification: table-driven EAC permit/deny check for the new
// `acl` setter role (docs/adr/0005-acl-record-schema.md), mirroring
// gate2.1-eac-check.ts's technique exactly but scoped to just this one role
// rather than mutating that already-published, already-verified table.
// Only an account holding ROLE_SET_TEXT scoped to the acl setter may write
// the `acl` record — enroller/rotator/revoker (Gate 2.1's roles) and a
// no-role account must all be denied. Uses simulateContract (Gate 0.3's
// proven technique — an eth_call with a `from` override, no signature
// needed) rather than sending real transactions for the deny cases.
//
// Run manually against Sepolia — costs real gas for the four setup grants,
// depends on live testnet state, not wired into pnpm verify/CI.
//
// Usage: bun run test/acl-eac-check.ts
// Requires SEPOLIA_PRIVATE_KEY in .env to hold ALL_ROLES on the fixture
// device's resolver (true for whoever ran scripts/provision-dev-tailnet —
// the resolver deployer gets ALL_ROLES on it). Does not need
// BRAMBLE_ACL_PEPPER — this test only checks who may call setText on the
// `acl` key, not the content written.
import { createWalletClient, http, toHex } from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import { privateKeyToAccount, generatePrivateKey } from 'viem/accounts'
import fixture from '../../sidecar/test/fixtures/dev-tailnet.json'

process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry

import { RPC_URL } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from '../src/setup'
import { resolverWriteAbi, pubkeySetter, revokedSetter, aclSetter } from '../src/roles'

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

  // Five fresh, never-funded, never-signing test accounts — simulateContract
  // only needs an address (Gate 0.3's exact pattern). rotator/revoker prove
  // the acl role is independently scoped from Gate 2.1's two roles, not just
  // from "no role at all."
  const rotator = privateKeyToAccount(generatePrivateKey())
  const revoker = privateKeyToAccount(generatePrivateKey())
  const aclSetterAcct = privateKeyToAccount(generatePrivateKey())
  const noRole = privateKeyToAccount(generatePrivateKey())
  console.log('Test accounts:')
  console.log(`  rotator:    ${rotator.address}`)
  console.log(`  revoker:    ${revoker.address}`)
  console.log(`  aclSetter:  ${aclSetterAcct.address}`)
  console.log(`  noRole:     ${noRole.address}`)

  const deviceFullname = normalize(fixture.deviceFullname)
  const resolverAddress = await publicClient.getEnsResolver({ name: deviceFullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${deviceFullname}`)
  const dnsName = toHex(packetToBytes(deviceFullname))

  console.log('\n[setup 1/3] Granting ROLE_SET_TEXT(pubkey) to rotator...')
  await writeAndWait('grantSetterRoles(pubkey)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [pubkeySetter(), rotator.address],
  })

  console.log('\n[setup 2/3] Granting ROLE_SET_TEXT(revoked) to revoker...')
  await writeAndWait('grantSetterRoles(revoked)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [revokedSetter(), revoker.address],
  })

  console.log('\n[setup 3/3] Granting ROLE_SET_TEXT(acl) to aclSetter...')
  await writeAndWait('grantSetterRoles(acl)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [aclSetter(), aclSetterAcct.address],
  })

  const accounts = { rotator, revoker, aclSetter: aclSetterAcct, noRole } as const

  console.log('\n================ TABLE ================')
  for (const [name, acct] of Object.entries(accounts)) {
    let ok: boolean
    try {
      await publicClient.simulateContract({
        account: acct.address,
        address: resolverAddress,
        abi: resolverWriteAbi,
        functionName: 'setText',
        args: [dnsName, 'acl', 'acl-eac-check-test-digest'],
      })
      ok = true
    } catch {
      ok = false
    }
    report(`${name} -> set-acl`, name === 'aclSetter' ? ok : !ok)
  }

  console.log('\n================ ACL ROLE SUMMARY ================')
  const allPassed = Object.values(results).every(Boolean)
  console.log(
    allPassed
      ? '✅ ALL CHECKS PASSED — acl is governed by its own EAC role, independent from pubkey/revoked.'
      : '❌ AT LEAST ONE CHECK FAILED — see above.'
  )
  process.exit(allPassed ? 0 : 1)
}

main().catch((err) => {
  console.error('\nacl EAC check failed:', err)
  process.exit(1)
})
