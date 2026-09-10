// Mirrors sidecar/src/ens/client.ts's DeviceRecord and
// sidecar/src/ens/relays.ts's Relays — kept as plain duplicated types rather
// than a shared package, since this dashboard deliberately never imports
// sidecar code (it only ever talks to it over HTTP, through explicit
// GET-only Route Handlers under /api/sidecar).
export interface DeviceRecord {
  fullname: string
  pubkey: string | null
  status: number
  expiry: string
  tokenId: string
  revoked: boolean
  acl: string[]
  aclGranters: string[]
}

export interface RendezvousRelay {
  label: string
  address: string
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

export interface SidecarHealth {
  ok: boolean
  tailnetName: string
  tailnetRegistry: string
}

// relay-sidecar's GET /price (relay-sidecar/src/pricing.ts's priceForBytes).
export interface AssetAmount {
  asset: string
  amount: string
}

export interface ApiError {
  error: string
}
