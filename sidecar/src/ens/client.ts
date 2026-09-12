import { createPublicClient, http, keccak256, toHex } from 'viem'
import { sepolia } from 'viem/chains'
import { normalize } from 'viem/ens'
import { RPC_URL, UNIVERSAL_RESOLVER, registryAbi, tailnetName, tailnetRegistry } from './config'

// viem ships a built-in Universal Resolver address for Sepolia that points at
// the production/beta ENSv2 deployment, not the hackathon one. Overwriting it
// is required — skipping this makes every resolution call silently target the
// wrong contracts. See docs/04_TECH_STACK.md, "Universal Resolver gotcha."
const hackathonSepolia = {
  ...sepolia,
  contracts: {
    ...sepolia.contracts,
    ensUniversalResolver: { address: UNIVERSAL_RESOLVER },
  },
}

export const publicClient = createPublicClient({ chain: hackathonSepolia, transport: http(RPC_URL) })

export interface DeviceRecord {
  fullname: string
  pubkey: string | null
  status: number
  expiry: string
  tokenId: string
  // revoked mirrors the `revoked` text record — an EAC-gated revocation
  // lever distinct from clearing `pubkey`, so revoke and rotate can be
  // granted to different accounts (see docs/adr/0004). Not the same field
  // as `status` above, which is the registry's own token-status enum.
  revoked: boolean
  // acl mirrors the `acl` text record — a list of ECDH digests (see
  // docs/adr/0005-acl-record-schema.md's aclDigestECDH), never plaintext
  // service names, hosts, IPs, or ports. EAC-gated by its own
  // grantSetterRoles-scoped role, independent from pubkey/revoked. Read-only
  // plumbing: this sidecar never sees or computes digests, just returns
  // whatever opaque strings are on chain. Empty/absent parses to [] (fail-
  // closed: no listed capabilities).
  acl: string[]
  // aclGranters mirrors the `acl-granters` text record — *this device's own*
  // list of already-enrolled identity labels it accepts ACL vouches from
  // when it acts as a gateway (docs/adr/0005). Independent EAC role from
  // acl above: acl controls who may write anything about this device at
  // all; acl-granters controls whose digests this device, in its gateway
  // role, actually honors. Meaningless for a device that never serves
  // anything locally — harmless to be empty in that case.
  aclGranters: string[]
  // The device's own dedicated Permissioned Resolver (enroll.ts deploys a
  // fresh one per device, never a shared one — see docs/adr/0004) — worth
  // surfacing on its own: it's the concrete, on-chain-verifiable evidence
  // that "each device gets its own resolver" isn't just a design claim.
  // null only if the name has no resolver set at all (shouldn't happen for
  // an enrolled device; resolveDevice() is still called against arbitrary
  // labels, so this stays nullable rather than throwing).
  resolverAddress: `0x${string}` | null
  // meshIP mirrors the `mesh-ip` text record — this device's WireGuard
  // tunnel address, as a CIDR string (e.g. "10.77.0.5/24"), matching the
  // exact format brambled's own -peer/-local-addr flags already parse
  // (docs/adr/0009). Written once, by the enrolling operator, at enroll
  // time — never self-service like pubkey/acl, since a device that could
  // move its own address could steal another device's, the same
  // label-hijack failure mode docs/adr/0005's requester-binding fix closed
  // for ACL digests. null for a device enrolled before this record existed.
  meshIP: string | null
}

export async function resolveDevice(label: string): Promise<DeviceRecord> {
  const fullname = normalize(`${label}.${tailnetName()}`)

  // Text records resolve across the whole naming hierarchy automatically via
  // the Universal Resolver — proven end to end by Gate 0.3 ("subname text
  // record round-trips through full hierarchy").
  const pubkey = await publicClient.getEnsText({ name: fullname, key: 'pubkey' })
  const revokedText = await publicClient.getEnsText({ name: fullname, key: 'revoked' })
  const aclText = await publicClient.getEnsText({ name: fullname, key: 'acl' })
  const aclGrantersText = await publicClient.getEnsText({ name: fullname, key: 'acl-granters' })
  const meshIP = await publicClient.getEnsText({ name: fullname, key: 'mesh-ip' })
  // getEnsResolver's own type says non-nullable, but an unset name returns
  // the zero address rather than throwing or returning null (same "falsy
  // check catches it" convention already used at every other
  // getEnsResolver call site in this repo, e.g. set-acl.ts) — normalize to
  // null here so the dashboard doesn't have to know that quirk too.
  const resolverAddressRaw = await publicClient.getEnsResolver({ name: fullname })
  const resolverAddress =
    resolverAddressRaw && resolverAddressRaw !== '0x0000000000000000000000000000000000000000'
      ? resolverAddressRaw
      : null

  // getState() is registry-specific, not hierarchy-resolved — it must be
  // called against the subname's own subregistry contract, which this node's
  // sidecar is configured with (BRAMBLE_TAILNET_REGISTRY), not discovered by
  // walking the chain. getState() is proven (Gate 0.3) to accept a bare
  // labelhash of the name's own label, unlike ownerOf() which requires the
  // exact current (version-matched) token ID.
  const labelhash = BigInt(keccak256(toHex(label)))
  const [status, expiry, , tokenId] = await publicClient.readContract({
    address: tailnetRegistry(),
    abi: registryAbi,
    functionName: 'getState',
    args: [labelhash],
  })

  return {
    fullname,
    pubkey,
    status,
    expiry: expiry.toString(),
    tokenId: tokenId.toString(),
    revoked: revokedText === 'true',
    acl: (aclText ?? '')
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0),
    aclGranters: (aclGrantersText ?? '')
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0),
    resolverAddress,
    meshIP,
  }
}
