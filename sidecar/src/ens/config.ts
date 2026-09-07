// Hackathon ENSv2 Sepolia deployment addresses.
// Source: https://feature-permres-inode-refact.docs-bao.pages.dev/learn/deployments#sepolia-ensv2-beta
// Cross-checked against Kevin | ENS's Discord post, Sept 4 2026, and verified
// end to end by scripts/gate0.3-eac-check/index.mjs on Sept 5 2026.
//
// This is the hackathon-specific deployment, not the production/beta one —
// see docs/04_TECH_STACK.md, "Hackathon ENSv2 Sepolia deployment."
import { getAddress, parseAbi } from 'viem'

export const ETH_REGISTRY = getAddress('0x1d78834d97c1d7b1a38c1dedbd1a287cfed3971e') // Permissioned Registry for .eth
export const ETH_REGISTRAR = getAddress('0x7d1b7f586a62ac3f54b9a396849757814283270b')
export const MOCK_USDC = getAddress('0xcbfd80f74375c54e545af34788ff465f96f66f05')
export const VERIFIABLE_FACTORY = getAddress('0x894bc9cc8ff1ad96b8a288c86a8c71d662c07780')
export const PERMISSIONED_RESOLVER_IMPL = getAddress('0xa9d3814ab151bf6e37a427432795371a8361614e')
export const USER_REGISTRY_IMPL = getAddress('0x47b442d0cf617c41cabaff5f02f44dd1e5f72546')
export const UNIVERSAL_RESOLVER = getAddress('0xd26f2040d083af1cd2962ba303f4bea0c4faf142') // UpgradableUniversalResolverProxy

// EAC role constants (values from the ENSv2 docs' Permissioned Registry /
// Permissioned Resolver role tables, not guessed).
export const ROLE_SET_SUBREGISTRY = 1n << 20n
export const ROLE_SET_RESOLVER = 1n << 24n
export const ROLE_CAN_TRANSFER_ADMIN = (1n << 28n) << 128n
export const REGISTRATION_ROLE_BITMAP =
  ROLE_SET_SUBREGISTRY |
  (ROLE_SET_SUBREGISTRY << 128n) | // ROLE_SET_SUBREGISTRY_ADMIN
  ROLE_SET_RESOLVER |
  (ROLE_SET_RESOLVER << 128n) | // ROLE_SET_RESOLVER_ADMIN
  ROLE_CAN_TRANSFER_ADMIN

export const ROLE_SET_TEXT = 1n << 4n
export const ROLE_SET_ADDRESS = 1n << 0n

// Registry-level role gating who may call register() to enroll a new device
// subname — a ROOT_RESOURCE role on the subregistry contract itself, NOT
// part of REGISTRATION_ROLE_BITMAP above (which is what's granted to the
// *new token's owner*, a separate concern). Verified from primary source,
// not the docs page (same discipline as the initializer-signature drift in
// docs/12_SOURCE_NOTES.md): ensdomains/contracts-v2's
// contracts/src/registry/libraries/RegistryRolesLib.sol defines
// `ROLE_REGISTRAR = 1 << 0`, and PermissionedRegistry._register() calls
// `_checkRoles(ROOT_RESOURCE, RegistryRolesLib.ROLE_REGISTRAR, msg.sender)`
// for a fresh (never-registered) label. Granted via grantRootRoles(), not
// grantRoles() — see docs/adr/0004-admin-cli-writes-directly-to-registry.md.
export const ROLE_REGISTRAR = 1n << 0n
export const ROLE_REGISTRAR_ADMIN = ROLE_REGISTRAR << 128n

// Every regular + admin role bit set (bit 0 of all 64 nybbles), computed
// rather than copy-pasted, to avoid a uint256-overflow typo.
export const ALL_ROLES = BigInt('0x' + '1'.repeat(64))

export const RPC_URL = process.env.SEPOLIA_RPC_URL || 'https://ethereum-sepolia-rpc.publicnode.com'

export const SIDECAR_PORT = Number(process.env.BRAMBLE_SIDECAR_PORT || 7890)

// A node's sidecar belongs to exactly one tailnet, and must know which
// subregistry contract governs it — this is bootstrap config, not a record
// schema decision (Phase 2 still owns record schema / wildcard-resolution
// discretion, see docs/05_BUILD_PLAN.md Phase 2). getState() is a
// registry-specific call, unlike text-record reads which resolve across the
// whole hierarchy automatically via the Universal Resolver.
function requiredEnv(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required (sidecar config, not chain state)`)
  return value
}

export function tailnetName(): string {
  return requiredEnv('BRAMBLE_TAILNET_NAME')
}

export function tailnetRegistry(): `0x${string}` {
  return getAddress(requiredEnv('BRAMBLE_TAILNET_REGISTRY'))
}

// Read-only surface only — no write functions here. This sidecar's Section A
// scope is resolution, not enrollment/revocation (see docs/adr/0002).
export const registryAbi = parseAbi([
  'function ownerOf(uint256 id) view returns (address)',
  'function getState(uint256 anyId) view returns (uint8 status, uint64 expiry, address latestOwner, uint256 tokenId, uint256 resource)',
  'function roles(uint256 anyId, address account) view returns (uint256)',
])

// EAC role state: a verified read ABI now exists (Phase 2 finding,
// correcting the prior "no verified read ABI yet" note left after Gate 0.3).
// `roles(anyId, account)` returns the effective role bitmap for an account
// on a name, confirmed against ensdomains/contracts-v2's
// EnhancedAccessControl.sol/PermissionedRegistry.sol source and the ENSv2
// docs' "Enhanced Access Control" page. Not required for Gate 2.1's
// permit/deny test (Gate 0.3's simulateContract pattern already proves
// that), but available for anything that wants to display or assert current
// role state directly rather than inferring it from a simulated call.
