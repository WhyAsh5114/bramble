// Device enumeration for the tailnet's own subregistry — the read half of
// docs/adr/0009's mesh-ip allocation scheme. Same LabelRegistered-log-scan
// pattern already proven for relay discovery (./relays.ts): PermissionedRegistry
// has no enumeration view function (no totalSupply/tokenByIndex), but it
// emits LabelRegistered(tokenId, labelHash, label, owner, expiry, sender)
// with the plaintext label on every registration.
//
// This lives in the sidecar's read-only surface (see config.ts's registryAbi
// comment: "resolution, not enrollment/revocation") — the only consumer of
// listDeviceLabels today is admincli's enroll.ts, deciding which mesh-ip
// addresses are already taken before writing a new one.
import { parseAbiItem } from 'viem'
import { publicClient } from './client'
import { tailnetRegistry, tailnetRegistryDeployBlock } from './config'

const labelRegisteredEvent = parseAbiItem(
  'event LabelRegistered(uint256 indexed tokenId, bytes32 indexed labelHash, string label, address owner, uint64 expiry, address indexed sender)'
)

// listDeviceLabels returns every label ever registered on the tailnet's
// device subregistry, deduplicated (a label can be re-registered after its
// prior registration expires — see resolveRelays()'s identical dedup for
// why a duplicate log entry doesn't mean a duplicate device).
export async function listDeviceLabels(): Promise<string[]> {
  const logs = await publicClient.getLogs({
    address: tailnetRegistry(),
    event: labelRegisteredEvent,
    fromBlock: tailnetRegistryDeployBlock(),
    toBlock: 'latest',
  })
  return [...new Set(logs.map((log) => log.args.label).filter((label): label is string => !!label))]
}
