# Test hole punching behind two NATs

Use this guide to put two Bramble nodes behind two different NATs and to force the relay fallback. It serves W8 and demo beat 4. All steps need root. The development machine has no user namespaces and no `sudo`, so every step below is **Open** unless marked otherwise. The one exception is the Docker route in "Run the sponsored-relay check without root", which is Verified.

Labels: Verified, Read from source, Open (see `../README.md`).

## Choose a setup

| Setup                | Where                           | Use it for                                                 |
| -------------------- | ------------------------------- | ---------------------------------------------------------- |
| `netns-lab.sh`       | One Linux machine with root     | Developing W8 without cloud machines. Six namespaces.      |
| `scripts/nat-box.sh` | The gateway and worker machines | The real demo topology. Each node sits behind its own NAT. |

Both use Linux nftables source NAT. `cone` uses `masquerade`, which keeps the source port when it is free. `symmetric` uses `masquerade random`, which picks a new source port per flow. Why these two: `../TOPOLOGY.md`.

## Local lab

Needs `iproute2`, `nftables` and `python3`. The lab has an uplink by default: the `inet` namespace connects to the host (`198.18.0.0/30`), the host source-NATs the lab to the internet, and `nodeA`, `nodeB` and `server` get a resolver file. Nodes therefore reach the Sepolia proxy by name, as on the cloud machines. `NETLAB_UPLINK=0` builds an isolated lab; the nodes then cannot read ENS and need test fixtures. Open (no step has run: the development machine has no root).

1. Build the lab: `sudo deploy/nat-lab/netns-lab.sh up cone`. Open.
2. Check it: `sudo deploy/nat-lab/netns-lab.sh status`. Open.
3. Run the relay and STUN server in the `server` namespace (address `192.0.2.10`) and one `brambled` in each of `nodeA` and `nodeB`, with `ip netns exec <ns> ...`. Give each node its own state directory and key, and set `SEPOLIA_RPC_URL` to the proxy URL. Open.
4. Check the NAT type from `nodeA` with the probe in the "Check the NAT type" section, using `192.0.2.10:3478` and `192.0.2.20:3478`. Both sit behind the node's NAT only, so the host's masquerading cannot change the result. Run the STUN server in the `server` namespace listening on each address separately (for `coturn`: two `listening-ip` lines with `stun-only`), so each reply comes from the address that was queried. Do not use a public server here: it adds the host's own NAT to the path and can show a false symmetric result. Open.
5. Repeat from step 1 with `symmetric` to see the relay fallback. Open.
6. Remove everything: `sudo deploy/nat-lab/netns-lab.sh down`. Open.

## Run the sponsored-relay check without root (Docker)

`docker-lab.sh` builds the same six namespaces inside a short-lived Docker container, so the host network is untouched and the host needs no `sudo`. It needs `docker`, `go` and `bun` on the host. Verified on 6 Nov 2026 on the development machine (ETH-189): the container needs `NET_ADMIN`, `SYS_ADMIN` and `systempaths=unconfined` (so `ip_forward` is writable inside its own namespaces).

```sh
deploy/nat-lab/docker-lab.sh
```

It runs `netns-lab.sh up symmetric` with `NETLAB_UPLINK=0`, starts the real `relay` binary with `-meter -data-relay -sponsor-enrolled` in the `server` namespace, and runs two nodes:

1. Control: `nodeA` and `nodeB` point WireGuard at each other's NAT address. The handshake must fail.
2. Relay run: direct UDP between the NATs is also dropped (`block-direct`). `nodeA` (the device, no Hedera key) and `nodeB` (the cloud node) take sponsored tokens, the device takes a sponsored session, and the device must get HTTP 200 from a server on the cloud node.

The relay's sidecar is the test harness `relay-sidecar/test/e2e-harness`. It mounts the real sponsor routes, token minting and allocation. Its registry read is a fixture, because the lab has no route to a chain. The script exits non-zero unless the control prints `NETLAB_RESULT direct-control: no handshake` and the device then prints `NETLAB_RESULT relay: HTTP 200`. If the control connects or crashes, the script stops before the relay run.

## Cloud NAT box

Run on both the gateway and the worker machine. The node then reaches the internet through source NAT on its own host.

1. Create the box: `sudo deploy/scripts/nat-box.sh up cone`. Open.
2. Move every loopback-coupled unit into it by copying `deploy/systemd/nat-box.conf.example` to `/etc/systemd/system/<unit>.service.d/nat-box.conf`, then `sudo systemctl daemon-reload` and restart. Open.
   - Gateway machine: both `bramble-demo-service` and `bramble-gateway`. The gateway dials `127.0.0.1:<DEMO_SERVICE_PORT>` in its own namespace (`brambled/gateway/server.go`), so `demo-service` must share the namespace. Read from source.
   - Worker machine: `bramble-worker`.
3. Confirm that the node's traffic leaves with the host's address: `sudo ip netns exec bramble-node curl -sS https://ifconfig.me`. Open.
4. Check the node from inside: `sudo NETNS=bramble-node deploy/scripts/smoke.sh gateway` (or `worker`). The node's status API (`127.0.0.1:7899`), sidecar and forward are loopback ports inside the namespace, so a host-side `curl` cannot see them. Open.
5. Start the agent on the worker with `sudo deploy/scripts/agent-in-box.sh claude`, which enters the namespace and then drops to the `agent` user. Open.
6. Enable the box at boot: `sudo systemctl enable --now bramble-nat-box`. Open.

Inbound packets reach the node only through a mapping the node created first, so the peer must use the address that STUN reports, not the host's address with port 51820. Read from source (nftables semantics).

## Check the NAT type

`deploy/scripts/stun-probe.py` binds one UDP socket and sends a STUN Binding request to each server from that socket. A different mapped address and port (IP:port) per server then means the NAT maps per destination (symmetric-like). Two separate clients are not enough: each would use a different source port, and even a cone NAT then shows different mapped endpoints.

```sh
sudo ip netns exec bramble-node python3 deploy/scripts/stun-probe.py \
  <relays-machine-public-name>:3478 <second-stun-server>:3478
```

Exit code 0 means the mapped IP:port endpoint was the same for every server, 1 means at least one endpoint differed (in IP or port), 2 means a server did not answer. On the cloud NAT box the second destination comes from item 14 in `../README.md` (a second STUN server, or a second public address of the relays machine); both destinations must be reached through the same NAT boundary. Open.

Evidence: `python3 deploy/scripts/stun-probe.py --selftest` ran on the development machine on 6 Nov 2026 against local fake servers and classified a same-mapping case, a different-port case and a different-address, same-port case correctly. That checks the request encoding, the response parsing and the comparison. It does not test a real NAT or `coturn`, so the probe against real servers is Open.

The probe diagnoses the NAT with its own socket. The W8 discovery gate (handoff section 5.4) must query from the WireGuard socket itself; that belongs to ETH-36 and is Open.

## Force the relay fallback

Beat 4 needs the direct path blocked on demand.

1. Block direct UDP to the peer: `sudo deploy/scripts/nat-box.sh block-direct <peer-public-ip>`. This drops forwarded UDP to that address, so the relay (a different address) still works. Open.
2. Watch the node's status API switch (inside the namespace: `sudo ip netns exec bramble-node curl -sS http://127.0.0.1:7899/peers`) the path from direct to relay. The status field names are Read from the W8 branch (ETH-36), unmerged. Open.
3. Restore: `sudo deploy/scripts/nat-box.sh unblock-direct`. Open.

The ruleset can be printed without root: `deploy/scripts/nat-box.sh print-rules symmetric <peer-ip>`. Verified on 6 Nov 2026 that it prints; `nft -c` could not fully check it here (netlink permission denied), so syntax is parse-checked only.

## Remove the box

`sudo deploy/scripts/nat-box.sh down`. Open.
