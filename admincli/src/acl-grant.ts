// Shared preamble for set-acl.ts and revoke-acl.ts: decrypts and verifies
// the granter's Key Ring-protected key, resolves the gateway/device
// pubkeys, and computes the ECDH digest for each service name
// (docs/adr/0005-acl-record-schema.md). A revoke needs to compute the exact
// same digests a grant would have — same inputs, same algorithm — so both
// scripts share this rather than duplicating it and risking drift.
import { readFileSync } from 'node:fs'
import { normalize } from 'viem/ens'
import { x25519 } from '@noble/curves/ed25519'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils'
import { tailnetName } from '../../sidecar/src/ens/config'
import { publicClient } from './setup'
import { aclDigestECDH } from './roles'
import { ringDecrypt } from './ledger-ring'
import { GRANTER_KEY_RING_PATH } from './granter-key-path'

export interface GrantDigests {
  granterFullname: string
  gatewayFullname: string
  deviceFullname: string
  digests: string[]
}

export async function computeGrantDigests(
  deviceLabel: string,
  gatewayLabel: string,
  granterLabel: string,
  serviceNames: string[]
): Promise<GrantDigests> {
  let granterCiphertext: Buffer
  try {
    granterCiphertext = readFileSync(GRANTER_KEY_RING_PATH)
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

  // The digest is bound to the requesting device's own pubkey (closes the
  // copy-a-digest-into-my-own-record gap — see docs/adr/0005): acl records
  // are public, so without this binding any device able to write its own
  // acl record could copy a digest read off another device's public record
  // and gain the same grant.
  const deviceFullname = normalize(`${deviceLabel}.${tailnetName()}`)
  const devicePubkey = await publicClient.getEnsText({ name: deviceFullname, key: 'pubkey' })
  if (!devicePubkey) throw new Error(`no pubkey found for device ${deviceFullname} — is it enrolled?`)

  const digests = serviceNames.map((name) => aclDigestECDH(granterPrivKeyHex, gatewayPubkey, devicePubkey, name))
  return { granterFullname, gatewayFullname, deviceFullname, digests }
}
