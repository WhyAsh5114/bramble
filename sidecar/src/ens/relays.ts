// Real relay discovery (docs/adr/0003's Consequence section, resolved) —
// replaces the -rendezvous/-data-relay hardcoding that shipped before this.
//
// PermissionedRegistry has no enumeration view function (no totalSupply/
// tokenByIndex — checked against ensdomains/contracts-v2's
// IStandardRegistry.sol directly, not assumed), but it emits
// LabelRegistered(tokenId, labelHash, label, owner, expiry, sender) on every
// registration, and `label` is the plaintext string — confirmed by querying
// eth_getLogs against the real dev tailnet registry and getting real labels
// back. Scanning that event on the relay registry (a distinct, structurally
// separate contract from the tailnet's device registry — see
// admincli/src/setup-relay-registry.ts, Gate 1.4) is the whole discovery
// mechanism: no manifest text record needed.
import { parseAbiItem } from 'viem'
import { normalize } from 'viem/ens'
import { publicClient } from './client'
import { scanLogsChunked } from './scan'
import { relayRegistry, relayRegistryDeployBlock, tailnetName } from './config'

const labelRegisteredEvent = parseAbiItem(
  'event LabelRegistered(uint256 indexed tokenId, bytes32 indexed labelHash, string label, address owner, uint64 expiry, address indexed sender)'
)

export interface RendezvousRelay {
  label: string
  address: string
  // A relay's own payment sidecar, if it published one — needed to buy a
  // rendezvous token valid at *this* relay specifically (each relay mints
  // under its own process-local secret, docs/adr/0008's failover fix).
  // Absent for an unmetered relay, which needs no token at all.
  sidecarUrl?: string
}

export interface DataRelay {
  label: string
  sidecarUrl: string
  pricePerByte: string
}

export interface Relays {
  rendezvous: RendezvousRelay[]
  dataRelays: DataRelay[]
}

// resolveRelays scans the relay registry for every registered relay label,
// then resolves each one's own text records (rendezvous-address / sidecar-
// url / price-per-byte, whichever it published — a relay can be either
// role, or both). Returns empty lists, not an error, if no relay registry
// is configured — callers (brambled) treat that the same as "discovery
// found nothing," falling back to their own -rendezvous/-data-relay
// overrides if set.
export async function resolveRelays(): Promise<Relays> {
  const registry = relayRegistry()
  if (!registry) return { rendezvous: [], dataRelays: [] }

  // Chunked via scanLogsChunked (see its doc comment), not a single
  // fromBlock-to-latest call — Infura's free tier started rejecting that
  // form outright once this registry's history grew past 10,000 blocks
  // (found live, Sept 12 2026, the same day devices.ts hit the identical
  // bug).
  const logs = await scanLogsChunked(publicClient, {
    address: registry,
    event: labelRegisteredEvent,
    fromBlock: relayRegistryDeployBlock(),
  })

  // A label can be re-registered (its prior registration expired, then
  // someone registers the same string again) — getLogs returns one
  // LabelRegistered per registration, so a re-registered label would
  // otherwise appear twice in this scan. Collapse to unique labels first;
  // each one's text records are resolved fresh against current chain state
  // below regardless of which log entry surfaced it, so a duplicate log
  // entry would only ever have produced a duplicate, never stale, result —
  // but a duplicate entry in the returned list is still wrong on its own.
  const uniqueLabels = new Set(logs.map((log) => log.args.label).filter((label): label is string => !!label))

  const rendezvous: RendezvousRelay[] = []
  const dataRelays: DataRelay[] = []

  for (const label of uniqueLabels) {
    const fullname = normalize(`${label}.relays.${tailnetName()}`)

    const [address, sidecarUrl, pricePerByte] = await Promise.all([
      publicClient.getEnsText({ name: fullname, key: 'rendezvous-address' }),
      publicClient.getEnsText({ name: fullname, key: 'sidecar-url' }),
      publicClient.getEnsText({ name: fullname, key: 'price-per-byte' }),
    ])

    if (address) rendezvous.push({ label, address, sidecarUrl: sidecarUrl ?? undefined })
    if (sidecarUrl) dataRelays.push({ label, sidecarUrl, pricePerByte: pricePerByte ?? '1' })
  }

  return { rendezvous, dataRelays }
}
