// Grants a device an ACL capability — appends to its `acl` text record
// (docs/adr/0005-acl-record-schema.md), an EAC-gated capability list
// distinct from pubkey/revoked (its own grantSetterRoles-scoped role,
// docs/adr/0004's pattern). Symbolic only: a service name, never a host,
// IP, or port — Section C's gateway maps a name to a local port using local
// config, not anything published here.
//
// What actually goes on chain is a Diffie-Hellman digest, not the plaintext
// name and not a value derived from any manually-shared secret: it's an
// ECDH shared secret between the *granter's* (an already-enrolled identity
// vouching for this grant) and the *gateway's* (the device being asked to
// serve it) already-published WireGuard pubkeys — see roles.ts's
// aclDigestECDH. The gateway later verifies by recomputing the identical
// shared secret from its own private key plus the granter's already-public
// pubkey; nothing is ever transmitted or copied by hand. Whether the
// gateway actually trusts this granter is a separate, gateway-owned
// decision — see set-acl-granters.ts.
//
// Usage: bun run set-acl.ts <device-label> <gateway-label> <granter-label> <comma,separated,service,names>
//
// The granter identity's own WireGuard private key (bare hex, no 0x — the
// same kind of key any enrolled device already has, not a new secret type)
// is never read as plaintext from .env (Gate 3.2, docs/adr/0001-ledger-
// ring-vs-send-split.md's "Pivot" section): it's decrypted in-memory,
// per call, from a wallet-cli Key Ring-encrypted file via
// provision-granter-key.ts. Run that once first.
import { readFileSync } from 'node:fs'
import { createWalletClient, http, toHex } from 'viem'
import { normalize, packetToBytes } from 'viem/ens'
import { x25519 } from '@noble/curves/ed25519'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils'
import { RPC_URL, tailnetName } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { resolverWriteAbi, aclDigestECDH } from './roles'
import { ringDecrypt } from './ledger-ring'
import { GRANTER_KEY_RING_PATH } from './granter-key-path'

async function main() {
  const [deviceLabel, gatewayLabel, granterLabel, servicesArg] = process.argv.slice(2)
  if (!deviceLabel || !gatewayLabel || !granterLabel || servicesArg === undefined) {
    throw new Error(
      'usage: bun run set-acl.ts <device-label> <gateway-label> <granter-label> <comma,separated,service,names>'
    )
  }
  const serviceNames = servicesArg
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)

  let granterCiphertext: string
  try {
    granterCiphertext = readFileSync(GRANTER_KEY_RING_PATH, 'utf8').trim()
  } catch {
    throw new Error(
      `no ring-encrypted granter key at ${GRANTER_KEY_RING_PATH} — run ` +
        `\`BRAMBLE_GRANTER_PRIVATE_KEY=<hex> bun run provision-granter-key\` once first`
    )
  }
  const granterPrivKeyHex = (await ringDecrypt(granterCiphertext)).replace(/^0x/, '')
  const granterFullname = normalize(`${granterLabel}.${tailnetName()}`)
  const granterPubkeyOnChain = await publicClient.getEnsText({ name: granterFullname, key: 'pubkey' })
  const derivedGranterPubkey = bytesToHex(x25519.getPublicKey(hexToBytes(granterPrivKeyHex)))
  if (granterPubkeyOnChain !== derivedGranterPubkey) {
    throw new Error(
      `BRAMBLE_GRANTER_PRIVATE_KEY doesn't match ${granterFullname}'s published pubkey ` +
        `(on-chain: ${granterPubkeyOnChain ?? '(unset)'}, derived: ${derivedGranterPubkey}) — ` +
        `wrong key, or ${granterLabel} isn't the identity you meant to vouch as`
    )
  }

  const gatewayFullname = normalize(`${gatewayLabel}.${tailnetName()}`)
  const gatewayPubkey = await publicClient.getEnsText({ name: gatewayFullname, key: 'pubkey' })
  if (!gatewayPubkey) throw new Error(`no pubkey found for gateway ${gatewayFullname} — is it enrolled?`)

  const newDigests = serviceNames.map((name) => aclDigestECDH(granterPrivKeyHex, gatewayPubkey, name))
  console.log(`granter: ${granterFullname}`)
  console.log(`gateway: ${gatewayFullname}`)
  console.log(`services: ${serviceNames.join(', ') || '(none)'} -> digests: ${newDigests.join(',') || '(empty)'}`)

  const account = accountFromEnv()
  const wallet = createWalletClient({ account, chain: hackathonSepolia, transport: http(RPC_URL) })

  const fullname = normalize(`${deviceLabel}.${tailnetName()}`)
  const resolverAddress = await publicClient.getEnsResolver({ name: fullname })
  if (!resolverAddress) throw new Error(`no resolver found for ${fullname}`)
  const dnsName = toHex(packetToBytes(fullname))

  // acl is a growing capability set, not a single overwritten value (unlike
  // pubkey/revoked) — merge with whatever's already there instead of
  // clobbering grants from other (gateway, granter) pairs.
  const before = await publicClient.getEnsText({ name: fullname, key: 'acl' })
  const existingDigests = (before ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
  const value = Array.from(new Set([...existingDigests, ...newDigests])).join(',')
  console.log(`current acl digests for ${fullname}: ${before || '(unset)'}`)

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
  console.error('\nset-acl failed:', err)
  process.exit(1)
})
