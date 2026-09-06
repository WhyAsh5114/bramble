# brambled

The per-node mesh networking daemon. Spawns its own local ENS sidecar
(`../sidecar`, per `docs/adr/0002-node-agent-language.md`) and drives a
WireGuard device's peer table from it.

## Commands

- `brambled resolve <device-label>` — Phase 1 Section A. Prints a device
  subname's resolved ENS state (pubkey, expiry, status, tokenId).
- `brambled serve -peer <label>=<allowed-ip>/<prefix> [-peer ...]` — the
  admission verifier (Gate 1.1) plus, as of Section C, real connectivity.
  Starts a WireGuard device and keeps its peer table synced to the tracked
  labels' ENS state, polling every `-ttl` (default 30s). Flags:
  - `-local-addr` — this node's address and mesh subnet, CIDR (default
    `10.77.0.1/24`).
  - `-listen-port` — UDP port the WireGuard transport binds to (default
    `51820`).
  - **Real OS TUN is the default** — no flag needed. `-interface <name>`
    overrides the auto-picked interface name (`utun` on macOS, letting the OS
    assign a free number; `bramble0` on linux). This needs `sudo`
    (root/CAP_NET_ADMIN) — see "Known gaps" below for why this is the
    default rather than an opt-in.
  - `-netstack` — opt into a virtual (gVisor) TUN instead. Root-free, but not
    OS-visible (no real interface, so `ping`/`nc` from the shell can't reach
    it). **Testing and local dev only — never pass this for the actual demo
    or the Gate 0.2 run.** Automated tests always use this internally
    (`wgnode.Config.Netstack: true`), never `serve`'s default.
  - `-rendezvous <host:port>` — a rendezvous relay address (see `../relay`).
    When set, an authorized peer's endpoint is discovered by exchanging
    opaque candidates through the relay instead of relying on static config
    or dial-in-first. Omit for Section B's original static-peer behavior.
  - `-my-candidate <ip:port>` — this node's own reachable address to publish
    via the relay (e.g. a VPS's public IP and `-listen-port`). Only useful
    alongside `-rendezvous`; leave unset if this node has no address worth
    advertising (e.g. a laptop behind NAT that only needs to _reach out_, not
    be dialed into — see the runbook below).
- `brambled genkey` — generates a WireGuard key pair.

`BRAMBLE_TAILNET_NAME` and `BRAMBLE_TAILNET_REGISTRY` are required for
`resolve` and `serve` (see `../sidecar/README.md` — a node has to know which
tailnet it belongs to; this isn't discovered automatically).

## Packages

- `sidecar/` — spawns/health-checks the sidecar child process, HTTP client
  for its read endpoint.
- `wgnode/` — thin wrapper around `wireguard-go`'s `Device`. Real OS TUN by
  default (`tun.CreateTUN` + platform-specific `ifconfig`/`ip` interface
  configuration); `Config.Netstack: true` opts into a gVisor virtual TUN
  instead, used only by automated tests and CI. Add/remove peers via the
  UAPI config protocol, read the peer table back. `Ping`/`DialTCP`/`ListenTCP`
  are netstack-only probes for tests — a real-TUN node is verified with the
  OS's own `ping`/`nc` (see the runbook below).
- `admission/` — the resolver+cache loop (Gate 1.1). Polls ENS state for a
  fixed set of peer labels, adds/removes them from a `wgnode.Node`'s peer
  table. This loop _is_ the admission verifier — WireGuard's own protocol
  already refuses handshakes from unconfigured keys, so there's no separate
  accept/reject hook to write. `Loop.EndpointResolver`, if set, discovers an
  authorized peer's endpoint (e.g. via `rendezvous.Exchange`) once and caches
  it for the life of the loop.
- `rendezvous/` — client for the dumb rendezvous relay (`../relay`, a
  separate Go module — the two only share a wire protocol, not Go types).
  `Exchange(relayAddr, ownPubkey, peerPubkey, myCandidate, timeout)` publishes
  this node's candidate and returns the peer's, retrying until both sides'
  offers are acked (see `../relay/main.go`'s protocol comment for why the ack
  matters).

## Gate 0.2 runbook — two peers, two networks, no trusted coordinator

`docs/11_DAY0_GATES.md` Gate 0.2 requires a real laptop-to-VPS (or hotspot)
run: candidate exchange through a dumb relay, real `ping` + one TCP
connection across the tunnel, from two genuinely different networks. This
can't be executed from this environment (no VPS, no second physical network
available here) — the steps below are for whoever runs it.

**Topology:** VPS has a public IP and needs no NAT traversal on its side —
the easier, currently-supported case (`11_DAY0_GATES.md:30`). The relay runs
on the VPS too, since it already has a reachable address.

1. On the VPS, build and run the relay:
   ```
   cd relay && go build -o bin/relay . && ./bin/relay -addr :9420
   ```
2. On the VPS, run `brambled serve`, publishing its own public IP as its
   candidate:
   ```
   sudo BRAMBLE_TAILNET_NAME=... BRAMBLE_TAILNET_REGISTRY=... \
     ./bin/brambled serve \
     -peer laptop=10.77.0.2/32 \
     -rendezvous 127.0.0.1:9420 \
     -my-candidate <vps-public-ip>:51820
   ```
3. On the laptop, run `brambled serve` pointed at the VPS's relay (its
   public IP), with no `-my-candidate` — a laptop behind home NAT has
   nothing externally reachable worth advertising yet (STUN/hole-punching
   for that case is explicitly deferred, see "Known gaps"):
   ```
   sudo BRAMBLE_TAILNET_NAME=... BRAMBLE_TAILNET_REGISTRY=... \
     ./bin/brambled serve \
     -local-addr 10.77.0.2/24 \
     -peer vps=10.77.0.1/32 \
     -rendezvous <vps-public-ip>:9420
   ```
4. **Before trying to ping, check the interface actually came up right.**
   `serve`'s startup log prints the real interface name and the exact
   commands to check (`ifconfig <iface>` and `netstat -rn | grep <subnet>`)
   — confirm the address and a route for the local `/24` are both present on
   _each_ machine before moving on. This step doesn't exist to be thorough:
   real-OS-TUN addressing/routing has not been exercised end to end anywhere
   before this run (see "Known gaps" below) — this is where a misconfigured
   interface would first show up, and it's a much cheaper place to catch it
   than a failed ping with no other signal.
5. **Pass criteria**, from either machine's shell (not the in-process
   helpers, which only work in `-netstack` mode):
   ```
   ping 10.77.0.1   # or 10.77.0.2 from the VPS
   nc 10.77.0.1 <some-port>   # one real TCP byte round trip
   ```
   Both `brambled serve` logs should show `admission: <label>: authorized`
   for the other side.
6. Record the result in `docs/11_DAY0_GATES.md` — pass/fail, which
   topology, and the date — only once actually run this way. A CI-only or
   same-machine run does not count (see "Known gaps").

## Gate 1.3 runbook — measuring real revocation latency

`docs/05_BUILD_PLAN.md` Gate 1.3 requires the actual measured time from
revocation transaction confirmation to connection drop, published in this
README, for the harder and more attack-relevant case: a peer that's
**already connected**, not one that hasn't dialed in yet. The mechanism is
proven by `admission/gate1_3_test.go`'s
`TestGate1_3_RevocationDropsAlreadyConnectedPeer` — this section is for
getting a real number against the live dev-tailnet fixture. Reuses the same
VPS + laptop setup as the Gate 0.2 runbook above (no NAT-traversal concerns
here, so the two sides don't need to be on different networks — but real OS
TUN interfaces on the same host can't be trusted to `ping` correctly either,
per "Known gaps" below, so keep using the two real machines).

The revocation primitive is clearing a device's `pubkey` text record — the
only proven ENS write path this repo has (no `setExpiry`/`renew`-to-the-past
primitive is verified safe to use; ENSv2 renewals only extend). This is a
real, on-chain, reversible action:
[`scripts/provision-dev-tailnet/set-pubkey.ts`](../scripts/provision-dev-tailnet/set-pubkey.ts).

1. Start both sides as in the Gate 0.2 runbook, but with a short `-ttl` (e.g.
   `-ttl 5s`) so the measured window is small enough to be legible on camera.
   Confirm both sides show `admission: <label>: authorized` and a real `ping`
   round trip is working first.
2. From one machine, start a continuous ping at the other's mesh address and
   let it run in the foreground:
   ```
   ping 10.77.0.2   # from the VPS, pinging the laptop, or vice versa
   ```
3. From wherever you have `SEPOLIA_PRIVATE_KEY` configured, revoke the
   pinged side's device:
   ```
   cd scripts/provision-dev-tailnet && bun run set-pubkey.ts device2 ""
   ```
   Note the script's printed confirmation timestamp — that's t=0.
4. Watch three things and record their timestamps: the `set-pubkey.ts`
   confirmation time (t=0), the pinging side's `ping` output for the last
   successful reply and first failure, and the _other_ side's `brambled
serve` stderr for its `admission: device2: not authorized` line. The
   number to publish is the delta between t=0 and the last successful ping
   reply — that's the actually-connected-peer drop time the gate cares
   about, and per `TestGate1_3_RevocationDropsAlreadyConnectedPeer` it
   should be bounded by `-ttl`, not by WireGuard's ~2 minute rekey timer.
5. Restore the fixture afterward so future runs aren't left broken:
   ```
   bun run set-pubkey.ts device2 <pubkey set-pubkey.ts printed in step 3>
   ```
6. Record the measured number, the `-ttl` used, and the date in
   `docs/05_BUILD_PLAN.md`'s Gate 1.3 entry — only once actually run this
   way, same standard as Gate 0.2.

## Known gaps, deliberately not solved yet

- **Real OS TUN is the unconditional default, not a flag you have to
  remember.** The alternative — netstack by default, real TUN as an opt-in
  — has a failure mode worse than any bug: forgetting `-interface` on demo
  day would silently produce the virtual/in-process path, which looks
  identical to success until someone tries a real `ping` from the shell and
  gets nothing. Making netstack the thing you have to deliberately ask for
  (only in test code and never in `serve`'s default) removes that failure
  class entirely instead of documenting around it.
- **Real OS TUN addressing/routing is verified on Linux (Ubuntu 26.04
  arm64) and macOS, laptop-to-VPS topology only** (`11_DAY0_GATES.md` Gate
  0.2, ✅ 2026-09-06). A same-machine rehearsal was tried first and abandoned
  as a validation method — both nodes' addresses are local to one host
  there, so a shell `ping` resolves via local delivery (or fails
  ambiguously) without ever proving the tunnel path either way. The real
  two-machine run is what actually exercised `configureInterface` (which
  follows `wg-quick`'s own macOS/Linux convention: address assignment plus,
  on macOS, an explicit whole-local-subnet route — Linux's `ip addr add`
  creates its own kernel route automatically, no separate step needed).
- **STUN and hole punching aren't implemented.** The rendezvous relay
  carries candidate exchange, but there's no NAT traversal for the harder
  two-NAT case — only the laptop-to-VPS topology (VPS has a public IP,
  needs no traversal on its side) is currently supported end to end.
  `11_DAY0_GATES.md` Gate 0.2's own note explicitly separates this easier
  case from the harder one. Multi-relay failover is still deferred until this
  exists — Gate 1.2's hostile-relay test itself doesn't need it (see
  `docs/05_BUILD_PLAN.md` Gate 1.2, ✅ verified).
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
- **x402 metering of rendezvous relay usage is not implemented** — the
  relay is currently free to use. That's Phase 4's job.
