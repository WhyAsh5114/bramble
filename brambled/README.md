# brambled

The per-node mesh networking daemon. Spawns its own local ENS sidecar
(`../sidecar`, per `docs/adr/0002-node-agent-language.md`) and drives a
WireGuard device's peer table from it.

## Commands

- `brambled resolve <device-label>` — Phase 1 Section A. Prints a device
  subname's resolved ENS state (pubkey, expiry, status, tokenId).
- `brambled serve -peer <label>=<allowed-ip>/<prefix> [-peer ...]` — Phase 1
  Section B, Gate 1.1's admission verifier. Starts a WireGuard device
  (netstack-backed — no root required) and keeps its peer table synced to
  the tracked labels' ENS state, polling every `-ttl` (default 30s).
- `brambled genkey` — generates a WireGuard key pair.

`BRAMBLE_TAILNET_NAME` and `BRAMBLE_TAILNET_REGISTRY` are required for
`resolve` and `serve` (see `../sidecar/README.md` — a node has to know which
tailnet it belongs to; this isn't discovered automatically).

## Packages

- `sidecar/` — spawns/health-checks the sidecar child process, HTTP client
  for its read endpoint.
- `wgnode/` — thin wrapper around `wireguard-go`'s `Device`: create (netstack
  TUN + real UDP transport), add/remove peers via the UAPI config protocol,
  read the peer table back, `Ping` for handshake-success probes in tests.
- `admission/` — the resolver+cache loop (Gate 1.1). Polls ENS state for a
  fixed set of peer labels, adds/removes them from a `wgnode.Node`'s peer
  table. This loop _is_ the admission verifier — WireGuard's own protocol
  already refuses handshakes from unconfigured keys, so there's no separate
  accept/reject hook to write.

## Known gaps, deliberately not solved yet

- **Authorization ignores the registry's `status` field**, using only
  `pubkey != nil && expiry > now`. `status`'s enum meaning isn't documented
  anywhere verified (our one fixture reads back `2`) — see
  `admission/loop.go`'s package comment. Revocation may need `status` once
  its meaning is confirmed; Phase 2 owns EAC/registry-state semantics.
- **`serve`'s peer list is static CLI config**, not resolved from ENS.
  Discovering "which peers belong to my tailnet" automatically is a record
  schema question Phase 2 owns (`docs/05_BUILD_PLAN.md` Phase 2 discretion).
- **No key persistence.** `serve` generates an ephemeral key if
  `-private-key`/`BRAMBLE_PRIVATE_KEY` isn't set — a real node's identity
  eventually comes from enrollment (Ledger-backed, `docs/adr/0001`), not from
  this daemon.
- **No cross-machine connectivity yet.** `serve` only manages the peer
  table; STUN, hole punching, and the rendezvous relay (Gate 1.2) aren't
  implemented, so two `serve` instances on different networks can't yet find
  each other's real endpoint. `wgnode`'s own test proves handshake
  admission/refusal using known loopback endpoints, not discovery.
