# brambled

The per-node mesh networking daemon. Spawns its own local ENS sidecar
(`../sidecar`, per `docs/adr/0002-node-agent-language.md`) and drives a
WireGuard device's peer table from it.

## Commands

- `brambled resolve <device-label>` — Phase 1 Section A. Prints a device
  subname's resolved ENS state (pubkey, expiry, status, tokenId).
- `brambled serve -label <own-label> -peer <label>=<allowed-ip>/<prefix> [-peer ...]` —
  the admission verifier (Gate 1.1) plus, as of Section C, real connectivity
  and gateway/ACL enforcement. Starts a WireGuard device and keeps its peer
  table synced to the tracked labels' ENS state, polling every `-ttl`
  (default 30s). Flags:
  - `-label <own-label>` — **required.** This node's own ENS label, used to
    resolve its own `acl-granters` record for gateway enforcement
    (`docs/adr/0005-acl-record-schema.md`).
  - `-local-addr` — this node's address and mesh subnet, CIDR (default
    `10.77.0.1/24`).
  - `-listen-port` — UDP port the WireGuard transport binds to (default
    `51820`).
  - `-service <name>=<local-port>` (repeatable) — a local, non-ENS service
    this node offers as a gateway; omit to offer none. A peer whose ACL
    grants it that symbolic name gets proxied to `127.0.0.1:<local-port>`
    (`docs/adr/0006-gateway-connect-protocol.md`).
  - `-gateway-port` — fixed TCP port the CONNECT gateway listener binds to
    on this node's own tunnel address (default `7892`). Must match on both
    sides of a `-forward` — see below.
  - `-forward <local-port>=<gateway-label>:<service>` (repeatable) — a local
    port-forward, `ssh -L`-style: an ordinary, unmodified local client
    (`psql`, `curl`, an agent's own HTTP client — anything with no idea this
    proxy exists) connects to `127.0.0.1:<local-port>` and transparently
    reaches `<gateway-label>`'s `<service>`, CONNECT-wrapped and ACL-checked
    on its behalf. `<gateway-label>` must also be a tracked `-peer` —
    `serve` fails fast at startup otherwise. See "Client forwarding runbook"
    below for a real two-machine example.
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
  - `-data-relay <label>=<relay-sidecar-url>` (repeatable) and
    `-relay-peer <peer-label>` (repeatable) — select an explicit paid
    data-relay pool for peers that cannot use the direct candidate. Each
    purchase requests `-relay-session-bytes` (default `10000`).
  - `-max-relay-price-per-byte` (default `10` atomic USDC) and
    `-max-relay-session-cost` (default `100000`, or 0.10 USDC) — reject an
    otherwise-discovered relay quote before purchase if either budget is
    exceeded. The local payment sidecar independently enforces
    `HEDERA_MAX_PAYMENT_ATOMIC` per x402 payment.
- `brambled genkey` — generates a WireGuard key pair.

The demo also includes two deliberately small helper processes under `cmd/`:
`demo-service` exposes a private health task, while `demo-agent` calls it
through a local Bramble forward and validates the result. The agent has no
backend credential; a denied grant, a successful grant, and revocation are
therefore visible as changes to the same command's outcome.

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

`docs/10_DAY0_GATES.md` Gate 0.2 requires a real laptop-to-VPS (or hotspot)
run: candidate exchange through a dumb relay, real `ping` + one TCP
connection across the tunnel, from two genuinely different networks. This
can't be executed from this environment (no VPS, no second physical network
available here) — the steps below are for whoever runs it.

**Topology:** VPS has a public IP and needs no NAT traversal on its side —
the easier, currently-supported case (`10_DAY0_GATES.md:30`). The relay runs
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
6. Record the result in `docs/10_DAY0_GATES.md` — pass/fail, which
   topology, and the date — only once actually run this way. A CI-only or
   same-machine run does not count (see "Known gaps").

## Client forwarding runbook — an unmodified client through a real gateway

`docs/adr/0006-gateway-connect-protocol.md`'s `Forwarder` exists so that a
real client with no idea `CONNECT`/ACLs/ENS exist — `curl`, `psql`, an
agent's own HTTP client — can still reach a mesh service. Reuses the exact
Gate 0.2 laptop-to-VPS topology and relay above; add `-service`/`-forward`
to each side's existing `serve` command.

1. On the VPS (the gateway), alongside step 2 of the Gate 0.2 runbook, add
   `-service web=<local-port-of-a-real-backend>` to the existing `serve`
   command.
2. On the laptop, add `-forward 8080=vps:web` to its existing `serve`
   command.
3. Grant the ACL for real, on-chain, via `admincli`'s already-verified
   tooling (`docs/adr/0005-acl-record-schema.md`): `set-acl-granters.ts` on
   the VPS's own resolver (trust a granter), then `set-acl.ts` writing the
   `web` digest to the laptop's device record.
4. **Pass criteria, from the laptop's shell — an ordinary client, zero
   CONNECT wiring:**
   ```
   curl http://127.0.0.1:8080/
   ```
   should return the real backend's real response, proxied laptop →
   relay-discovered tunnel → VPS gateway → ACL check → local backend, and
   back. Remove or never grant the digest and re-run `curl` — it should
   fail (connection reset/refused), proving the denial path holds for a
   real, unmodified client too, not just test code that hand-writes
   `CONNECT`.
5. Record the result below once actually run — pass/fail and the date,
   same evidentiary standard as Gate 0.2/1.3 above.

**Run Sept 8, 2026 — pass.** VPS: `Bramble` (AWS EC2, `ap-south-1`,
`13.207.155.93`, arm64, real OS TUN, `sudo`). Laptop: this machine, `-netstack`
(no sudo needed — the local `-forward` listener is always a real OS socket
regardless of the node's own TUN mode; only the tunnel-side dial differs,
and WireGuard's cross-machine transport is real UDP either way, so netstack
on one end doesn't weaken the "two real machines, real internet" claim).
Two freshly enrolled real devices on the dev tailnet (`vps-demo`,
`laptop-demo`, `bramble-dev-c91e55e9.eth`), a real `acl-granters`/`acl`
digest pair written on-chain via `admincli`'s `set-acl-granters.ts`/`set-acl.ts`
(granter: the already-enrolled `device1`), a real relay-mediated candidate
exchange over the actual internet (no static endpoint config on either
side).

```
curl http://127.0.0.1:8080/hello-from-a-real-curl
→ demo-service on ip-172-31-28-162, request path=/hello-from-a-real-curl, served at 2026-09-08T17:22:58Z
  [HTTP 200, 2.21s]
```

An ordinary `curl` — no `CONNECT`, no ACL, no ENS awareness of any kind —
reached a real Go HTTP server running on the VPS, laptop → relay → real
WireGuard tunnel → gateway's CONNECT listener → real on-chain ACL check →
`127.0.0.1:9100` on the VPS → back. Gateway log: `laptop-demo allowed for
"web" — proxying to 127.0.0.1:9100`. Forwarder log: `"web" allowed —
proxying`.

**Denial counterpart, same run:** cleared `laptop-demo`'s on-chain `acl`
record (a direct `setText(acl, '')`, bypassing `set-acl.ts`'s merge
behavior), re-ran the identical `curl` with no other change — `curl: (56)
Recv failure: Connection reset by peer`. Gateway log: `laptop-demo denied
for service "web" (no matching granted digest)`. Forwarder log: `"web"
denied by gateway`. No restart of either `brambled` process was needed
between the allow and deny runs — `CheckACL` resolves fresh chain state on
every `CONNECT`, so revocation-style edits take effect on the very next
request.

Full design and buffering-correctness fix behind `Forwarder`:
`docs/adr/0006-gateway-connect-protocol.md`.

## Gate 2.2 runbook — nothing hard-coded in the demo path

`docs/05_BUILD_PLAN.md` Gate 2.2 requires that the mesh reassemble purely
from chain state, with no local cache to go stale. Two complementary
artifacts back this claim:

- `scripts/check-no-local-state.mjs` (wired into `pnpm verify`) — a
  structural grep proving the demo _runtime_ path (`brambled/**/*.go`
  excluding tests, `sidecar/src/**/*.ts`) contains no filesystem-write call
  at all. Deliberately out of scope: `admincli` and
  `scripts/provision-dev-tailnet`, which are operator tooling, not anything
  a node runs.
- `brambled/admission/gate2_2_test.go`
  (`TestGate2_2_FreshLoopReadsCurrentChainStateNotPriorMemory`) — the
  behavioral half: a `Loop` constructed with zero prior state admits
  exactly what the resolver reports _now_, carries no memory of a
  pre-restart key, and re-derives revocation after a restart too.

**State inventory** (what actually exists around a running node, and why
none of it is a cache):

| Item                                                                     | What it is                                                                                                                                                                                                                                                       |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `brambled`, `demo-service`, `relay` binaries                             | Build artifacts — code, not state. Rebuilding from the same source produces the same behavior.                                                                                                                                                                   |
| `sidecar/node_modules`                                                   | Dependencies, resolved from `sidecar/bun.lock` — reproducible, not node-specific state.                                                                                                                                                                          |
| `start-vps-gateway.sh`                                                   | Operator launch config: CLI flags and env vars. Never read by `brambled` itself at runtime beyond process startup — see the `BRAMBLE_PRIVATE_KEY` note below.                                                                                                    |
| `*.log` files (`relay.log`, `demo-service.log`, `brambled-vps.log`)      | Shell-redirected stdout from `nohup`, not written by any `os.Create`/`fs.write*` call in the demo path — `check-no-local-state.mjs` correctly doesn't flag these. Deleted as part of this run's own cleanup below, purely to satisfy the gate's literal wording. |
| Everything else (peer identity, ACL grants, admission state, revocation) | CLI flags, env vars, or chain state (ENS text records) — re-read on every sync (`-ttl`, default 30s) or every `CONNECT`.                                                                                                                                         |

`BRAMBLE_PRIVATE_KEY` sits in plaintext in `start-vps-gateway.sh` on the
VPS today — that's Gate 3.2's job (`wallet-cli ring` encryption), not
Gate 2.2's; noted here so it isn't mistaken for an oversight.

**Run Sept 9, 2026 — pass.** Same VPS as the Sept 8 run
(`13.207.155.93`, `ap-south-1`, arm64), laptop is this machine
(`-netstack`). `laptop-demo`'s original enrollment key had been lost
(overwritten testing infrastructure); rotated to a freshly generated
key via `admincli/src/rotate.ts` (real tx
`0x163f3019ad8296d23bd704d915d66bc0160d0ef6bed0ade397566c303bffc505`) and
its ACL grant for `vps-demo`'s `web` service re-vouched by `device1` via
`admincli/src/set-acl.ts` (real tx
`0x669b6b6d6d53718515c10ec152f327823346c49ade6cef38458c7da9623b8d41`) —
routine rotation/ACL operations, not part of the gate's own procedure.

1. **Phase A — baseline.** Both nodes started fresh (`relay`,
   `demo-service`, `brambled serve` on the VPS; `brambled serve -netstack
-forward 8080=vps-demo:web` on the laptop). Real `curl`:
   ```
   curl http://127.0.0.1:8080/gate2.2-phase-A-baseline
   → demo-service on ip-172-31-28-162, request path=/gate2.2-phase-A-baseline, served at 2026-09-09T05:37:13Z
     [HTTP 200, 2.66s]
   ```
2. **State wipe.** Both `brambled` processes killed. While both were down,
   `laptop-demo`'s on-chain `pubkey` record was cleared (`scripts/
provision-dev-tailnet/set-pubkey.ts laptop-demo ""`, tx
   `0x989c8feeeacfa90c34643e8e5dd2f517befa42d3cb40bb2ca361dd56aa73b63c`) —
   simulating a restarted node whose peer's identity changed while it was
   offline, with nothing local surviving the restart to contradict it.
3. **Phase B — fresh restart, same flags, no local state carried over.**
   The VPS's brand-new `Loop` (zero prior state, matching the unit test)
   refused the peer outright:
   ```
   admission: laptop-demo: not authorized
   ```
   `curl` timed out — a full connection timeout, not a quick ACL `DENY`,
   confirming the failure is at the WireGuard admission layer (the peer
   was never added to the tunnel), not the ACL layer above it:
   ```
   curl -m 8 http://127.0.0.1:8080/gate2.2-phase-B-should-fail
   → (timed out, exit 28)
   ```
4. **Phase A′ — restore, no restart.** `laptop-demo`'s `pubkey` was set
   back to its rotated value on chain (tx
   `0x34bb1d0d089ae6547505245dde6167aa0a1393af5ab3bb4ac5832f9ab498259c`)
   with **neither process restarted**. The already-running VPS `Loop`
   picked it up on its own next sync (≤30s later, `-ttl`'s default):
   ```
   admission: laptop-demo: authorized (pubkey 0031b7eafd9a67b0463a529663083aeb12806f9e0f2ef3f10c69787a90d3d904)
   gateway: laptop-demo allowed for "web" — proxying to 127.0.0.1:9100
   ```
   and a fresh `curl` succeeded with zero local file touched anywhere in
   the interim:
   ```
   curl http://127.0.0.1:8080/gate2.2-phase-Aprime-restored
   → demo-service on ip-172-31-28-162, request path=/gate2.2-phase-Aprime-restored, served at 2026-09-09T05:41:29Z
     [HTTP 200, 3.77s]
   ```
5. **Literal cleanup.** All three `*.log` files this run produced
   (`~/vps-run/relay.log`, `~/vps-run/demo-service.log`,
   `~/vps-run/brambled-vps.log`) were deleted while their processes kept
   running unaffected (Linux unlink semantics) — the state inventory table
   above is exhaustive, so there was nothing else to delete.

Both the admission-refusal (Phase B) and the memory-free restart
(`gate2_2_test.go`'s own three phases) agree: nothing survives a restart
except what's on chain.

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

## Gate 5.1 runbook — the agent path, live, two real machines

`docs/05_BUILD_PLAN.md` Hard Gate 5.1 requires a real two-machine run with a
real ENS write, not just `pnpm test:access-flow`'s in-memory-resolver
version. **Run Sept 10, 2026 — pass.** Laptop: this machine, `-netstack`,
agent identity `agent-1`. VPS: `Bramble` (`13.207.155.93`, real OS TUN),
already-enrolled `vps-demo`, offering `web` (real `demo-service`, not a
stub — reports its own hostname and a timestamp so a response provably
crossed the tunnel).

1. Enrolled `agent-1` fresh (`enroll.ts agent-1 <pubkey>`), no ACL —
   `register` tx `0x5653842b...d42b97`, `pubkey` tx `0x795b8cd9...8177f18`.
2. Restarted `vps-demo` with `agent-1` added to its own `-peer` flags (both
   sides need each other in their `-peer` list — this isn't ENS-discovered,
   see "Known gaps" below) and trusted `ledger-granter-test` as an
   additional `acl-granters` entry alongside the existing `device1`
   (`set-acl-granters.ts vps-demo device1,ledger-granter-test`, tx
   `0x0d69a8b3...318a806`).
3. **Denied, for real:** `demo-agent` against the forward failed; the VPS's
   own gateway logged `gateway: agent-1 denied for service "web" (no
matching granted digest)` — a real ACL check, not a network failure (the
   WireGuard handshake itself succeeds; the gateway layer is what refuses).
4. **Granted, for real:** `set-acl.ts agent-1 vps-demo ledger-granter-test
web` — decrypts the granter's key from the Ledger Key Ring headlessly
   (Gate 3.2), no device attached for this step. Tx `0x20ae7109...30bfa`.
5. **Succeeded, without restarting anything:** same `demo-agent` invocation
   now printed `task complete: report-private-service-health is healthy on
ip-172-31-28-162 (observed ...)` — the VPS's gateway logged `gateway:
agent-1 allowed for "web" — proxying to 127.0.0.1:9100`.
6. **Revoked, for real:** `revoke.ts agent-1 true`, tx
   `0xb4e0cf91...51db0f`. Next resync (`-ttl` default 30s, no override used
   this run): VPS logged `admission: agent-1: not authorized` and the same
   `demo-agent` request failed again.
7. Reset for the next run (including tomorrow's recording): `revoke.ts
agent-1 false` (tx `0x16ccb2f6...141e0178a`) and cleared `agent-1`'s
   `acl` text record back to empty (tx `0xd9bfa8f1...4c2edb57052`) — so the
   demo starts from the same denied state again, not mid-story.

Two real, pre-existing bugs surfaced and fixed by this run, not staged:

- `brambled/main.go`'s `sidecarConfig()` hardcoded its spawned sidecar to
  port 7890, so a second `brambled serve` on the same machine (`agent-1`,
  alongside the already-running `laptop-demo`) couldn't start its own
  sidecar at all. Now reads `BRAMBLE_SIDECAR_PORT` if set, same env var the
  spawned child already expected.
- The VPS's `demo-service` binary predated the `/task` JSON-response
  handler (`6351201`) — it was still the earlier generic echo build, so
  requests reached it fine but got a plain-text response `demo-agent`
  couldn't parse. Rebuilt for `linux/arm64` and redeployed.

**Gotcha worth knowing, not a bug:** `-gateway-port` isn't just "the port
this node's own gateway binds to" — a node's forward mechanism dials a peer
using _its own_ `-gateway-port` value as the assumed port on that peer too
(`main.go`'s forward dial, `node.DialTCP(peerAddr, gatewayPort)`). Every
node sharing a tailnet must use the same `-gateway-port`, or forwards to
mismatched-port peers fail with a connection error that looks like a
network problem, not a config mismatch.

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
  arm64) and macOS, laptop-to-VPS topology only** (`10_DAY0_GATES.md` Gate
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
  `10_DAY0_GATES.md` Gate 0.2's own note explicitly separates this easier
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
- **Resolver errors fail open by default.** If the sidecar dies or its RPC
  breaks, `SyncOnce` logs the error but does not by itself remove an
  already-admitted peer — Gate 1.3's "bounded by TTL" revocation-latency
  claim implicitly assumes sidecar+RPC liveness. `-max-stale` bounds this
  (remove a peer once resolution has been failing for that long); it
  defaults to `0` (unbounded fail-open, unchanged prior behavior). See
  `admission/loop.go`'s package comment for the full reasoning, and set
  `-max-stale` for any real demo or deployment rather than relying on the
  default.
