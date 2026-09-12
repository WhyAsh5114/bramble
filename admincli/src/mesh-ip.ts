// Mesh-ip allocation policy (docs/adr/0009) — the write half of what
// sidecar/src/ens/devices.ts's listDeviceLabels enables. Kept as a small
// pure function, separate from enroll.ts's chain-calling code, so the
// allocation logic itself can be unit tested without touching the network.
//
// Network choice: matches the /24 already hardcoded as brambled's own
// -local-addr default (10.77.0.1/24, see brambled/main.go and
// brambled/README.md) — not a new convention, just finally deriving it
// instead of asking every operator to type it.
export const MESH_NETWORK_PREFIX = '10.77.0.'
export const MESH_PREFIX_BITS = 24
const MESH_HOST_MIN = 2 // .0 is the network address, .1 is free but skipped
// for tidiness — the point of auto-allocation is never having to think
// about which number is "already someone's," so starting at a round number
// costs nothing.
const MESH_HOST_MAX = 254 // .255 is the broadcast address

// parseMeshIPHost extracts the host octet from a mesh-ip CIDR string in
// exactly the format brambled's own -peer/-local-addr flags already parse
// (netip.ParsePrefix's "a.b.c.d/bits" — see docs/adr/0009's "one parser,
// one format" note). Returns null for anything outside MESH_NETWORK_PREFIX
// or unparseable, so a stale/foreign value can never collide with a real
// allocation by accident.
function parseMeshIPHost(cidr: string): number | null {
  if (!cidr.startsWith(MESH_NETWORK_PREFIX)) return null
  const hostPart = cidr.slice(MESH_NETWORK_PREFIX.length).split('/')[0]
  if (!/^\d+$/.test(hostPart)) return null
  const n = Number(hostPart)
  return Number.isInteger(n) ? n : null
}

// allocateMeshIP returns the lowest free host address in the mesh /24,
// given every mesh-ip CIDR string already in use (nulls/undefineds for
// devices with none yet are fine to pass straight through). Throws once
// the /24 is exhausted rather than silently reusing an address — 253
// usable hosts is far past anything a hackathon demo tailnet needs, so
// hitting this means something upstream (the scan itself) is wrong, not
// that the network is genuinely full.
export function allocateMeshIP(usedCIDRs: (string | null | undefined)[]): string {
  const used = new Set(
    usedCIDRs
      .filter((c): c is string => !!c)
      .map(parseMeshIPHost)
      .filter((n): n is number => n !== null)
  )
  for (let host = MESH_HOST_MIN; host <= MESH_HOST_MAX; host++) {
    if (!used.has(host)) return `${MESH_NETWORK_PREFIX}${host}/${MESH_PREFIX_BITS}`
  }
  throw new Error(
    `mesh /24 exhausted (${MESH_HOST_MAX - MESH_HOST_MIN + 1} host addresses all in use) — see docs/adr/0009`
  )
}
