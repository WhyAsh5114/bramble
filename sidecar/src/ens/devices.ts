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
import { parseAbiItem, type PublicClient } from 'viem'
import { publicClient } from './client'
import { scanLogsChunked } from './scan'
import { tailnetRegistry, tailnetRegistryDeployBlock } from './config'

const labelRegisteredEvent = parseAbiItem(
  'event LabelRegistered(uint256 indexed tokenId, bytes32 indexed labelHash, string label, address owner, uint64 expiry, address indexed sender)'
)

// listDeviceLabels returns every label ever registered on the tailnet's
// device subregistry, deduplicated (a label can be re-registered after its
// prior registration expires — see resolveRelays()'s identical dedup for
// why a duplicate log entry doesn't mean a duplicate device). Chunked via
// scanLogsChunked, not a single fromBlock-to-latest call — found live,
// Sept 12 2026, the moment the registry's history grew past 10,000 blocks
// and Infura started rejecting the single-call form outright.
//
// client defaults to the sidecar's own Sepolia client. Tests inject a
// fixture-backed client instead (sidecar/test/devices.test.ts), because
// public Sepolia nodes prune old logs (RPC error 4444). An RPC error
// propagates rather than returning a partial list: enroll.ts allocates a
// mesh-ip from this list, so a missing label could hand out an address that
// is already taken.
export async function listDeviceLabels(client: PublicClient = publicClient): Promise<string[]> {
  const logs = await scanLogsChunked(client, {
    address: tailnetRegistry(),
    event: labelRegisteredEvent,
    fromBlock: tailnetRegistryDeployBlock(),
  })
  return [...new Set(logs.map((log) => log.args.label).filter((label): label is string => !!label))]
}
