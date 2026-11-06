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
// serve it) already-published WireGuard pubkeys, with the requesting
// *device's* own pubkey mixed into the HMAC message alongside the service
// name — see roles.ts's aclDigestECDH. The gateway later verifies by
// recomputing the identical shared secret from its own private key plus the
// granter's already-public pubkey, and the requester's own already-public
// pubkey (resolved as part of the same lookup that finds its acl record);
// nothing is ever transmitted or copied by hand. The requester-pubkey
// binding is what stops a digest read off one device's public acl record
// from being copied verbatim into a different device's own record. Whether
// the gateway actually trusts this granter is a separate, gateway-owned
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
//
// This file is the CLI wrapper; the logic is lib.ts's grantAcl.
//
// To remove a specific grant instead of adding one, see revoke-acl.ts —
// same usage shape, set-subtraction instead of set-union.
import { grantAcl, parseServiceNames } from './lib'
import { cliDeps, loadGranterKeyFromRing } from './lib-viem'

async function main() {
  const [deviceLabel, gatewayLabel, granterLabel, servicesArg] = process.argv.slice(2)
  if (!deviceLabel || !gatewayLabel || !granterLabel || servicesArg === undefined) {
    throw new Error(
      'usage: bun run set-acl.ts <device-label> <gateway-label> <granter-label> <comma,separated,service,names>'
    )
  }
  await grantAcl(cliDeps(), {
    deviceLabel,
    gatewayLabel,
    granterLabel,
    serviceNames: parseServiceNames(servicesArg),
    loadGranterKey: loadGranterKeyFromRing,
  })
}

main().catch((err) => {
  console.error('\nset-acl failed:', err)
  process.exit(1)
})
