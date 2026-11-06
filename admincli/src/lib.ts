// Importable grant / revoke operations behind admincli's set-acl, revoke-acl
// and revoke commands. This module has no top-level side effects and does not
// import setup.ts, the Ledger Key Ring or the sidecar's env config, so callers
// (the approval service, the agent tools, tests) can import it directly.
//
// It does not implement any cryptography. The ECDH digest stays in roles.ts
// (aclDigestECDH, docs/adr/0005-acl-record-schema.md). Chain access goes
// through two small interfaces, so a caller supplies the real viem
// implementation (lib-viem.ts) or an in-memory one:
//
//   - Resolver reads text records.
//   - Signer submits a setText write and reports confirmation.
//
// The granter's X25519 private key comes from a `loadGranterKey` callback. The
// CLIs pass the Key Ring decrypt; this module never reads a key from disk.
import { normalize } from 'viem/ens'
import { x25519 } from '@noble/curves/ed25519'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils'
import { aclDigestECDH } from './roles'

/** Reads ENS text records. Returns `null` or `''` for an unset record. */
export interface Resolver {
  getText(name: string, key: string): Promise<string | null>
}

/** A submitted write. `wait()` rejects if the transaction reverts. */
export interface PendingTx {
  hash: string
  wait(): Promise<void>
}

/** Submits ENS text-record writes for a normalized full name. */
export interface Signer {
  setText(name: string, key: string, value: string): Promise<PendingTx>
}

/** Returns the granter's WireGuard (X25519) private key as bare or `0x` hex. */
export type LoadGranterKey = () => Promise<string>

export interface LibDeps {
  resolver: Resolver
  signer: Signer
  /** The tailnet's base ENS name, for example `mesh.eth`. */
  tailnetName: string
  /** Receives progress lines. The CLIs pass `console.log`. Defaults to silent. */
  log?: (line: string) => void
}

export interface GrantParams {
  deviceLabel: string
  gatewayLabel: string
  granterLabel: string
  serviceNames: string[]
  loadGranterKey: LoadGranterKey
}

export interface GrantDigests {
  granterFullname: string
  gatewayFullname: string
  deviceFullname: string
  digests: string[]
}

export interface AclChange {
  deviceFullname: string
  granterFullname: string
  gatewayFullname: string
  digests: string[]
  /** The device's `acl` record before the call. */
  before: string
  /** The device's `acl` record after the call. Equals `before` when nothing changed. */
  after: string
  /** `undefined` when no transaction was needed (revoke of an absent grant). */
  txHash: string | undefined
}

export interface SetRevokedResult {
  deviceFullname: string
  txHash: string
  /** Milliseconds from submit to confirmation. */
  confirmedMs: number
  after: string
}

const noop = () => {}

/** Splits a comma-separated service list, trimming blanks. */
export function parseServiceNames(arg: string): string[] {
  return arg
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
}

/** Splits an `acl` record into its digests, trimming blanks. */
export function parseAclRecord(record: string | null | undefined): string[] {
  return parseServiceNames(record ?? '')
}

function deviceName(label: string, tailnetName: string): string {
  return normalize(`${label}.${tailnetName}`)
}

/**
 * Computes the ECDH digest of each service name for a (device, gateway,
 * granter) triple, exactly as a grant would write it. Verifies that the
 * granter key matches the granter's published `pubkey` first.
 */
export async function computeGrantDigests(
  deps: Pick<LibDeps, 'resolver' | 'tailnetName'>,
  params: GrantParams
): Promise<GrantDigests> {
  const { resolver, tailnetName } = deps
  const { deviceLabel, gatewayLabel, granterLabel, serviceNames, loadGranterKey } = params

  const granterPrivKeyHex = (await loadGranterKey()).replace(/^0x/, '')
  const granterFullname = deviceName(granterLabel, tailnetName)
  const granterPubkeyOnChain = await resolver.getText(granterFullname, 'pubkey')
  const derivedGranterPubkey = bytesToHex(x25519.getPublicKey(hexToBytes(granterPrivKeyHex)))
  if (granterPubkeyOnChain !== derivedGranterPubkey) {
    throw new Error(
      `BRAMBLE_GRANTER_PRIVATE_KEY doesn't match ${granterFullname}'s published pubkey ` +
        `(on-chain: ${granterPubkeyOnChain ?? '(unset)'}, derived: ${derivedGranterPubkey}) — ` +
        `wrong key, or ${granterLabel} isn't the identity you meant to vouch as`
    )
  }

  const gatewayFullname = deviceName(gatewayLabel, tailnetName)
  const gatewayPubkey = await resolver.getText(gatewayFullname, 'pubkey')
  if (!gatewayPubkey) throw new Error(`no pubkey found for gateway ${gatewayFullname} — is it enrolled?`)

  // The digest binds to the requesting device's own pubkey, so a digest read
  // off another device's public acl record cannot be copied into this one
  // (docs/adr/0005).
  const deviceFullname = deviceName(deviceLabel, tailnetName)
  const devicePubkey = await resolver.getText(deviceFullname, 'pubkey')
  if (!devicePubkey) throw new Error(`no pubkey found for device ${deviceFullname} — is it enrolled?`)

  const digests = serviceNames.map((name) => aclDigestECDH(granterPrivKeyHex, gatewayPubkey, devicePubkey, name))
  return { granterFullname, gatewayFullname, deviceFullname, digests }
}

async function writeAcl(deps: LibDeps, deviceFullname: string, value: string): Promise<string> {
  const log = deps.log ?? noop
  const pending = await deps.signer.setText(deviceFullname, 'acl', value)
  log(`   tx: ${pending.hash}`)
  await pending.wait()
  const after = await deps.resolver.getText(deviceFullname, 'acl')
  if ((after ?? '') !== value) throw new Error(`acl did not round-trip: got ${JSON.stringify(after)}`)
  return pending.hash
}

/**
 * Adds the digests for `serviceNames` to the device's `acl` record, keeping
 * every digest already there (the record is a growing set, not an overwrite).
 */
export async function grantAcl(deps: LibDeps, params: GrantParams): Promise<AclChange> {
  const log = deps.log ?? noop
  const { granterFullname, gatewayFullname, deviceFullname, digests } = await computeGrantDigests(deps, params)
  log(`granter: ${granterFullname}`)
  log(`gateway: ${gatewayFullname}`)
  log(`device: ${deviceFullname}`)
  log(`services: ${params.serviceNames.join(', ') || '(none)'} -> digests: ${digests.join(',') || '(empty)'}`)

  const before = (await deps.resolver.getText(deviceFullname, 'acl')) ?? ''
  log(`current acl digests for ${deviceFullname}: ${before || '(unset)'}`)
  const value = Array.from(new Set([...parseAclRecord(before), ...digests])).join(',')

  const txHash = await writeAcl(deps, deviceFullname, value)
  log(`done. ${deviceFullname}'s acl digests are now ${value === '' ? '(empty)' : value}.`)
  return { deviceFullname, granterFullname, gatewayFullname, digests, before, after: value, txHash }
}

/**
 * Removes the digests for `serviceNames` from the device's `acl` record. It
 * computes the same digests a grant would, so it removes exactly that grant
 * and leaves every other digest alone. Sends no transaction when none of the
 * digests are present.
 */
export async function revokeAcl(deps: LibDeps, params: GrantParams): Promise<AclChange> {
  const log = deps.log ?? noop
  const { granterFullname, gatewayFullname, deviceFullname, digests } = await computeGrantDigests(deps, params)
  log(`granter: ${granterFullname}`)
  log(`gateway: ${gatewayFullname}`)
  log(`device: ${deviceFullname}`)
  log(`services: ${params.serviceNames.join(', ') || '(none)'} -> digests to remove: ${digests.join(',') || '(empty)'}`)

  const before = (await deps.resolver.getText(deviceFullname, 'acl')) ?? ''
  log(`current acl digests for ${deviceFullname}: ${before || '(unset)'}`)

  const existing = parseAclRecord(before)
  const toRemove = new Set(digests)
  const remaining = existing.filter((d) => !toRemove.has(d))
  if (remaining.length === existing.length) {
    log(`none of the computed digests were present in ${deviceFullname}'s acl — nothing to do.`)
    return { deviceFullname, granterFullname, gatewayFullname, digests, before, after: before, txHash: undefined }
  }
  const value = remaining.join(',')

  const txHash = await writeAcl(deps, deviceFullname, value)
  log(`done. ${deviceFullname}'s acl digests are now ${value === '' ? '(empty)' : value}.`)
  return { deviceFullname, granterFullname, gatewayFullname, digests, before, after: value, txHash }
}

/**
 * Sets (`true`) or clears (`false`) a device's `revoked` record. Needs a signer
 * holding the revoked-scoped setText role (docs/adr/0004).
 */
export async function setDeviceRevoked(
  deps: LibDeps,
  params: { deviceLabel: string; value: boolean }
): Promise<SetRevokedResult> {
  const log = deps.log ?? noop
  const value = String(params.value)
  const deviceFullname = deviceName(params.deviceLabel, deps.tailnetName)

  log(`setting revoked=${value} for ${deviceFullname}...`)
  const startedAt = Date.now()
  const pending = await deps.signer.setText(deviceFullname, 'revoked', value)
  log(`   tx: ${pending.hash}`)
  await pending.wait()
  const confirmedMs = Date.now() - startedAt
  log(`   confirmed ${confirmedMs}ms after submit`)

  const after = (await deps.resolver.getText(deviceFullname, 'revoked')) ?? ''
  if (after !== value) throw new Error(`revoked did not round-trip: got ${JSON.stringify(after)}`)
  log(`done. ${deviceFullname} revoked=${after}.`)
  return { deviceFullname, txHash: pending.hash, confirmedMs, after }
}
