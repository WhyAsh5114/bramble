// Gate 2.1 verification: table-driven EAC permit/deny test for enrollment,
// rotation, and revocation — one case per {enroller, rotator, revoker,
// no-role} account x {enroll, rotate, revoke} action. Only the diagonal may
// succeed (docs/05_BUILD_PLAN.md Gate 2.1). Uses Gate 0.3's proven technique
// (publicClient.simulateContract with `account` set to the account under
// test — an eth_call with a `from` override, no signature needed) rather
// than sending real transactions for the deny cases.
//
// Run manually against Sepolia, like scripts/gate0.3-eac-check — costs real
// gas for the three setup grants, depends on live testnet state, not wired
// into pnpm verify/CI.
//
// Usage: bun run test/gate2.1-eac-check.ts
// Requires SEPOLIA_PRIVATE_KEY in .env to hold ROLE_REGISTRAR_ADMIN on the
// tailnet's subregistry and ALL_ROLES on the fixture device's resolver —
// true for whoever ran scripts/provision-dev-tailnet (the resolver deployer
// gets ALL_ROLES on it; the subregistry deployer gets ALL_ROLES, which
// includes ROLE_REGISTRAR_ADMIN, on it).
import { createWalletClient, http, toHex } from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import { privateKeyToAccount, generatePrivateKey } from 'viem/accounts'
import fixture from '../../sidecar/test/fixtures/dev-tailnet.json'

// tailnetRegistry() reads BRAMBLE_TAILNET_REGISTRY from the environment
// (sidecar bootstrap config, not chain state — see config.ts's own comment).
// Set it from the fixture here, same pattern sidecar/test/device.test.ts
// uses, since this script isn't launched via brambled/the sidecar's normal
// env wiring.
process.env.BRAMBLE_TAILNET_NAME = fixture.tailnetName
process.env.BRAMBLE_TAILNET_REGISTRY = fixture.tailnetRegistry

import { RPC_URL, tailnetRegistry, ROLE_REGISTRAR } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from '../src/setup'
import { registryWriteAbi, resolverWriteAbi, pubkeySetter, revokedSetter } from '../src/roles'

const results: Record<string, boolean> = {}
function report(name: string, pass: boolean) {
  results[name] = pass
  console.log(`${pass ? '✅ PASS' : '❌ FAIL'} — ${name}`)
}

const zeroAddress = '0x0000000000000000000000000000000000000000' as const

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

  // Four fresh, never-funded, never-signing test accounts — simulateContract
  // only needs an address (Gate 0.3's exact pattern).
  const enroller = privateKeyToAccount(generatePrivateKey())
  const rotator = privateKeyToAccount(generatePrivateKey())
  const revoker = privateKeyToAccount(generatePrivateKey())
  const noRole = privateKeyToAccount(generatePrivateKey())
  console.log('Test accounts:')
  console.log(`  enroller: ${enroller.address}`)
  console.log(`  rotator:  ${rotator.address}`)
  console.log(`  revoker:  ${revoker.address}`)
  console.log(`  noRole:   ${noRole.address}`)

  const deviceFullname = normalize(fixture.deviceFullname)
  const resolverAddress = await publicClient.getEnsResolver({ name: deviceFullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${deviceFullname}`)
  const dnsName = toHex(packetToBytes(deviceFullname))

  console.log('\n[setup 1/3] Granting ROLE_REGISTRAR to enroller...')
  await writeAndWait('grantRootRoles(ROLE_REGISTRAR)', {
    address: tailnetRegistry(),
    abi: registryWriteAbi,
    functionName: 'grantRootRoles',
    args: [ROLE_REGISTRAR, enroller.address],
  })

  console.log('\n[setup 2/3] Granting ROLE_SET_TEXT(pubkey) to rotator...')
  await writeAndWait('grantSetterRoles(pubkey)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [pubkeySetter(), rotator.address],
  })

  console.log('\n[setup 3/3] Granting ROLE_SET_TEXT(revoked) to revoker...')
  await writeAndWait('grantSetterRoles(revoked)', {
    address: resolverAddress,
    abi: resolverWriteAbi,
    functionName: 'grantSetterRoles',
    args: [revokedSetter(), revoker.address],
  })

  // A fresh, never-registered label — simulateContract never commits state,
  // so the same label can be reused across all four accounts' enroll cases.
  const testLabel = `gate2-1-${Math.random().toString(16).slice(2, 10)}`
  const farExpiry = BigInt(Math.floor(Date.now() / 1000) + 365 * 24 * 60 * 60)

  const accounts = { enroller, rotator, revoker, noRole } as const

  console.log('\n================ TABLE ================')
  for (const [name, acct] of Object.entries(accounts)) {
    let ok: boolean

    try {
      // roleBitmap=0 so the only thing under test is the ROLE_REGISTRAR
      // check, not a separate "can this account grant these roles" check.
      await publicClient.simulateContract({
        account: acct.address,
        address: tailnetRegistry(),
        abi: registryWriteAbi,
        functionName: 'register',
        args: [testLabel, acct.address, zeroAddress, zeroAddress, 0n, farExpiry],
      })
      ok = true
    } catch {
      ok = false
    }
    report(`${name} -> enroll`, name === 'enroller' ? ok : !ok)

    try {
      await publicClient.simulateContract({
        account: acct.address,
        address: resolverAddress,
        abi: resolverWriteAbi,
        functionName: 'setText',
        args: [dnsName, 'pubkey', 'gate2.1-test-value'],
      })
      ok = true
    } catch {
      ok = false
    }
    report(`${name} -> rotate`, name === 'rotator' ? ok : !ok)

    try {
      await publicClient.simulateContract({
        account: acct.address,
        address: resolverAddress,
        abi: resolverWriteAbi,
        functionName: 'setText',
        args: [dnsName, 'revoked', 'true'],
      })
      ok = true
    } catch {
      ok = false
    }
    report(`${name} -> revoke`, name === 'revoker' ? ok : !ok)
  }

  console.log('\n================ GATE 2.1 SUMMARY ================')
  const allPassed = Object.values(results).every(Boolean)
  console.log(
    allPassed
      ? '✅ ALL CHECKS PASSED — enroll/rotate/revoke are governed by distinct EAC roles on the hackathon deployment.'
      : '❌ AT LEAST ONE CHECK FAILED — see above, this is a Gate 2.1 finding.'
  )
  process.exit(allPassed ? 0 : 1)
}

main().catch((err) => {
  console.error('\nGate 2.1 check failed:', err)
  process.exit(1)
})
