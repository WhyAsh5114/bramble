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
}

export async function resolveDevice(label: string): Promise<DeviceRecord> {
  const fullname = normalize(`${label}.${tailnetName()}`)

  // Text records resolve across the whole naming hierarchy automatically via
  // the Universal Resolver — proven end to end by Gate 0.3 ("subname text
  // record round-trips through full hierarchy").
  const pubkey = await publicClient.getEnsText({ name: fullname, key: 'pubkey' })
  const revokedText = await publicClient.getEnsText({ name: fullname, key: 'revoked' })

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
  }
}
