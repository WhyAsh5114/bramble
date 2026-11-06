# ADR 0015 — Cloud demo topology

**Status:** Draft, 6 November 2026. **Human approver:** Mumbai project human operator, pending; record their name and approval before accepting this ADR. **Task:** ETH-49 (W11b). **Demo beats:** all.

The demo runs on three small cloud machines (gateway with `demo-service`, agent worker, relays) and the admin machine, as the handoff fixes (section 5.3). This ADR records the choices the handoff leaves open. `deploy/TOPOLOGY.md` lists them in short form; `deploy/README.md` is the runbook.

## Context

The handoff names the machines but not where the RPC proxy, STUN server and public console URL live, how two NATs appear on three machines, or which privileges the node and agent run with. Each choice affects what the demo claims, what it costs, and which secrets sit on which machine. None of them has run on a cloud machine yet.

## Decision

1. **RPC proxy on the relays machine; nodes use its URL as `SEPOLIA_RPC_URL`.** Only the proxy and the admin machine hold the dedicated Sepolia endpoint, so no node file holds a credentialed URL. The admin machine calls the endpoint directly so that beat 6's log shows node traffic only.
2. **Public console URL through a reverse SSH tunnel from the admin machine to Caddy on the relays machine.** The approval service keeps its loopback bind. Only loopback is forwarded, with a dedicated key restricted on the relays machine.
3. **Two NATs from a network namespace on each of gateway and worker ("NAT box").** The host source-NATs the node's namespace with nftables: `masquerade` for a cone-like mapping, `masquerade random` for a symmetric-like one. In this mode `demo-service` joins the gateway's namespace and the agent starts through `agent-in-box.sh`, because loopback is per namespace.
4. **STUN server: coturn in `stun-only` mode on the relays machine.** Nodes read it from `BRAMBLE_STUN_SERVERS` (ETH-36, unmerged).
5. **Relay units are a systemd template (`bramble-relay@<instance>`); relay data sessions are pinned to UDP 40000-40999 by sysctl.** The data relay binds ephemeral ports (`relay/datarelay.go`), so the firewall rule needs a bounded range.
6. **Node units run as an unprivileged user with `CAP_NET_ADMIN`; the agent runs as a separate Unix user** with no read access to `/etc/bramble`.

## Alternatives considered

- **Third-party tunnel service for the console.** Needs an account and a second domain, and publishes the console through a vendor. Rejected for item 2.
- **Dedicated NAT router machines.** Realistic, but raises the count from three machines to five. Rejected for item 3.
- **A public STUN server.** It would see the demo nodes' addresses for no benefit. Rejected for item 4.
- **Nodes calling the dedicated endpoint directly.** Puts a credentialed URL on two more machines and leaves beat 6 without a node-only log. Rejected for item 1.
- **Run the nodes as root.** Simpler, and the September runs used `sudo`, but it widens the blast radius of a node bug. Kept as the documented fallback if the capability setup fails.

## Consequences

- A single NAT-box machine has a public address that the cloud provider also translates (one-to-one NAT), so the path is double NAT. The demo claims different NATs and does not claim a consumer router.
- STUN and the relays still see node IP addresses. The Tor upgrade covers the control plane only.
- The public console needs the admin machine online.
- The relays machine carries the proxy, Caddy, coturn and the relays together. Losing it ends beats 4 and 6.
- The NAT type check (`deploy/scripts/stun-probe.py`) uses its own UDP socket. The W8 discovery gate still has to query from the WireGuard socket.

## Verification

Verified on the development machine on 6 Nov 2026: the unit files parse, the scripts pass `bash -n`, the Go binaries build, `demo-service` and the relay start and answer on loopback, the env and key scripts behave as documented, and `stun-probe.py --selftest` classifies two local fake servers correctly.

Open: everything on real cloud machines, including the namespace NAT behaviour (`nat-box.sh` and `netns-lab.sh` need root), Caddy, coturn, the tunnel, the capability setup and the end-to-end demo. ETH-71 runs those steps.
