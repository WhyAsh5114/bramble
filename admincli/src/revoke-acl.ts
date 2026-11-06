// Revokes a specific ACL capability previously granted by set-acl.ts —
// removes the matching digest(s) from a device's `acl` text record
// (docs/adr/0005-acl-record-schema.md) instead of appending to it.
//
// Before this script existed there was no way to revoke a single grant at
// all: set-acl.ts only ever unions into the acl list. Revoking the granter
// identity itself (see the enroll/revoke flow, docs/adr/0004) stops that
// granter's *future* vouches and — as of Sept 11 2026 — its *past* ones too
// (gateway/acl.go's CheckACL now also checks !granter.Revoked), but neither
// touches a single already-published digest for an otherwise-still-trusted
// granter. This script is that missing removal path.
//
// Computes the exact same digest(s) set-acl.ts would have computed for the
// same (device, gateway, granter, services) inputs — see lib.ts's
// computeGrantDigests, shared by grantAcl and revokeAcl so the two can never
// drift — and filters them out of the existing acl list rather than adding
// to it. This file is the CLI wrapper; the logic lives in lib.ts.
//
// Usage: bun run revoke-acl.ts <device-label> <gateway-label> <granter-label> <comma,separated,service,names>
import { revokeAcl, parseServiceNames } from './lib'
import { cliDeps, loadGranterKeyFromRing } from './lib-viem'

async function main() {
  const [deviceLabel, gatewayLabel, granterLabel, servicesArg] = process.argv.slice(2)
  if (!deviceLabel || !gatewayLabel || !granterLabel || servicesArg === undefined) {
    throw new Error(
      'usage: bun run revoke-acl.ts <device-label> <gateway-label> <granter-label> <comma,separated,service,names>'
    )
  }
  await revokeAcl(cliDeps(), {
    deviceLabel,
    gatewayLabel,
    granterLabel,
    serviceNames: parseServiceNames(servicesArg),
    loadGranterKey: loadGranterKeyFromRing,
  })
}

main().catch((err) => {
  console.error('\nrevoke-acl failed:', err)
  process.exit(1)
})
