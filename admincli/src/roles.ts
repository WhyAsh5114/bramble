// EAC role-scoping helpers shared by every admincli command and by Gate
// 2.1's test. See docs/adr/0004-admin-cli-writes-directly-to-registry.md for
// the design this encodes: enrollment, rotation, and revocation are three
// distinct, separately-grantable EAC roles.
import { encodeFunctionData, parseAbi } from 'viem'

// Write surface — deliberately separate from sidecar/src/ens/config.ts's
// read-only registryAbi (that file's own comment: "Read-only surface only —
// no write functions here. This sidecar's Section A scope is resolution,
// not enrollment/revocation"). Mirrors the write-ABI duplication pattern
// already used in scripts/gate0.3-eac-check and scripts/provision-dev-tailnet.
export const registryWriteAbi = parseAbi([
  'function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256 tokenId)',
  'function grantRootRoles(uint256 roleBitmap, address account) returns (bool)',
])

export const resolverWriteAbi = parseAbi([
  'function setText(bytes name, string key, string value)',
  'function grantSetterRoles(bytes setter, address account) returns (bool)',
])

// pubkeySetter/revokedSetter encode the *setter identity* grantSetterRoles
// scopes a grant to. Only the function selector + record key matter to EAC
// — the name/value arguments here are placeholders, never actually sent on
// chain as a call. Proven pattern: scripts/gate0.3-eac-check/index.mjs used
// the identical technique to scope a grant to a "status" key.
export function pubkeySetter(): `0x${string}` {
  return encodeFunctionData({ abi: resolverWriteAbi, functionName: 'setText', args: ['0x', 'pubkey', ''] })
}

export function revokedSetter(): `0x${string}` {
  return encodeFunctionData({ abi: resolverWriteAbi, functionName: 'setText', args: ['0x', 'revoked', ''] })
}
