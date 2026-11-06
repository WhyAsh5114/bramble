# Deploy reference

Neutral facts for the demo cloud topology: machines, ports, files, units and variable names. For the steps, see `README.md`. Variable names only: no value belongs in this repository.

Labels: Verified, Read from source, Open (see `README.md`).

## Machines

| Machine   | Runs                                                                                                                             | Public inbound                                                           |
| --------- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `gateway` | `demo-service` (loopback `:9100`), `brambled serve` with `-service`, its spawned sidecar                                         | UDP 51820 (WireGuard), TCP 22 from the operator                          |
| `worker`  | `brambled serve` with `-forward` (loopback `:8080`), its spawned sidecar, the agent (Claude Code, MCP)                           | UDP 51820, TCP 22 from the operator                                      |
| `relays`  | `relay` with `-meter -data-relay` (one or two instances) and its spawned `relay-sidecar`, `rpc-proxy`, Caddy, coturn (STUN only) | TCP 80, 443, 9420, 7891; UDP 3478, 40000-40999; TCP 22 from the operator |
| `admin`   | approval service (loopback `:3001`), `admincli`, console tunnel client                                                           | none                                                                     |

Second relay instance: TCP 9430 and 7901 as well. Loopback-only ports (not exposed): sidecar `7890`, status API `7899`, relay internal API `7895`, gateway listener `7892` (bound to the tunnel address, not the public interface), `rpc-proxy` `8545`, console `3001`.

Source of each port: `brambled/main.go` flags (`-listen-port` 51820, `-gateway-port` 7892, `-status-port` 7899), `relay/main.go` flags (`-addr` 9420, `-sidecar-port` 7891, `-internal-port` 7895), `rpc-proxy/README.md` (8545), `approval/package.json` on the ETH-29 branch (3001). Read from source.

## Files in `deploy/`

| Path                                     | Purpose                                                        |
| ---------------------------------------- | -------------------------------------------------------------- |
| `README.md`                              | Runbook (how-to)                                               |
| `TOPOLOGY.md`                            | Why the topology looks this way (explanation)                  |
| `env/node.env.example`                   | Names for `/etc/bramble/gateway.env` and `worker.env`          |
| `env/relay.env.example`                  | Names for `/etc/bramble/relay-<instance>.env`                  |
| `env/admin.env.example`                  | Names for `/etc/bramble/admin.env`                             |
| `systemd/bramble-demo-service.service`   | `demo-service` on the gateway                                  |
| `systemd/bramble-gateway.service`        | Node, gateway role                                             |
| `systemd/bramble-worker.service`         | Node, worker role                                              |
| `systemd/bramble-relay@.service`         | Relay instance template (`bramble-relay@relay1`)               |
| `systemd/bramble-approval.service`       | Approval service on the admin machine                          |
| `systemd/bramble-console-tunnel.service` | Reverse SSH tunnel from admin to relays                        |
| `systemd/bramble-nat-box.service`        | Creates the NAT box at boot                                    |
| `systemd/nat-box.conf.example`           | Drop-in that moves a node unit into the NAT box                |
| `caddy/Caddyfile`                        | HTTPS for the RPC proxy and the console                        |
| `stun/turnserver.conf`                   | coturn as a STUN-only server                                   |
| `sysctl/90-bramble-relay.conf`           | Narrow ephemeral UDP range for relay sessions                  |
| `scripts/install.sh`                     | Build and install one role                                     |
| `scripts/check-env.sh`                   | Check an env file for required names (never prints values)     |
| `scripts/gen-node-key.sh`                | Create the node key in the env file; print the public key only |
| `scripts/smoke.sh`                       | Local liveness checks per role                                 |
| `scripts/nat-box.sh`                     | NAT in a network namespace on a cloud machine                  |
| `scripts/agent-in-box.sh`                | Run the agent as `agent` inside the NAT-box namespace          |
| `scripts/stun-probe.py`                  | Classify NAT mapping from one UDP socket (needs `python3`)     |
| `nat-lab/netns-lab.sh`                   | Full two-NAT lab on one machine                                |
| `nat-lab/README.md`                      | Two-NAT test runbook                                           |

The logging proxy's own files (`deploy/rpc-proxy/`) arrive with ETH-44 (PR #2).

## Paths on the machines

| Path                      | Content                                 |
| ------------------------- | --------------------------------------- |
| `/opt/bramble`            | Repository checkout and build outputs   |
| `/etc/bramble/*.env`      | Env files, mode `0600`, owner `bramble` |
| `/etc/systemd/system`     | Installed units                         |
| `/etc/netns/bramble-node` | Resolver file for the NAT box           |

## Environment variable names

Secret means: never in the repository, a ticket or a log. The env templates list names only. Optional names carry their default in the unit (`Environment=` lines); a line in the env file overrides it, and an empty line would override it with nothing (`check-env.sh` flags that). Defaults are in the Meaning column.

| Name                                    | Files        | Secret | Meaning                                                                |
| --------------------------------------- | ------------ | ------ | ---------------------------------------------------------------------- |
| `BRAMBLE_NODE_LABEL`                    | node         | no     | The node's own ENS label                                               |
| `BRAMBLE_TAILNET_NAME`                  | node, admin  | no     | Full ENS tailnet name                                                  |
| `BRAMBLE_TAILNET_REGISTRY`              | node, admin  | no     | Device registry address                                                |
| `BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK` | node, admin  | no     | First block to scan                                                    |
| `BRAMBLE_RELAY_REGISTRY`                | node, admin  | no     | Relay registry address                                                 |
| `BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK`   | node, admin  | no     | First block to scan                                                    |
| `BRAMBLE_PRIVATE_KEY`                   | node         | yes    | WireGuard private key (written by `gen-node-key.sh`)                   |
| `SEPOLIA_RPC_URL`                       | node         | no     | The logging proxy URL, never a credentialed endpoint                   |
| `SEPOLIA_RPC_URL`                       | admin, proxy | yes    | The dedicated Sepolia endpoint (the proxy calls it `UPSTREAM_RPC_URL`) |
| `SEPOLIA_PRIVATE_KEY`                   | admin        | yes    | Operator key that signs ENS writes                                     |
| `BRAMBLE_SIDECAR_DIR`                   | node         | no     | Sidecar project directory (`/opt/bramble/sidecar`)                     |
| `BRAMBLE_SIDECAR_PORT`                  | node         | no     | Sidecar port (7890)                                                    |
| `BRAMBLE_LISTEN_PORT`                   | node         | no     | WireGuard UDP port (51820)                                             |
| `BRAMBLE_TTL`                           | node         | no     | Re-resolve interval (`5s` for the demo)                                |
| `BRAMBLE_EXTRA_ARGS`                    | node         | no     | Extra `brambled serve` flags                                           |
| `BRAMBLE_STUN_SERVERS`                  | node         | no     | `host:port` list; read by the W8 branch (ETH-36), unmerged             |
| `BRAMBLE_SERVICE_NAME`                  | node         | no     | Service name offered (gateway) or requested (worker)                   |
| `DEMO_SERVICE_PORT`                     | gateway      | no     | Loopback port of `demo-service` (9100)                                 |
| `BRAMBLE_FORWARD_PORT`                  | worker       | no     | Local forward port (8080)                                              |
| `BRAMBLE_GATEWAY_LABEL`                 | worker       | no     | Gateway's ENS label                                                    |
| `HEDERA_CLIENT_ACCOUNT_ID`              | node         | no     | Testnet payer account ID                                               |
| `HEDERA_CLIENT_PRIVATE_KEY`             | node         | yes    | Testnet payer key                                                      |
| `HEDERA_MAX_PAYMENT_ATOMIC`             | node         | no     | Per-payment ceiling, atomic USDC (100000)                              |
| `RELAY_ADDR`                            | relay        | no     | Rendezvous listen address (`:9420`; second instance `:9430`)           |
| `RELAY_PAYEE_ACCOUNT_ID`                | relay        | no     | Hedera payee account ID (public)                                       |
| `RELAY_SIDECAR_PORT`                    | relay        | no     | Payment sidecar port (7891; second instance 7901)                      |
| `RELAY_INTERNAL_PORT`                   | relay        | no     | Loopback data-relay API port (7895; second instance 7905)              |
| `RELAY_PRICE_PER_BYTE`                  | relay        | no     | Atomic USDC per byte (1)                                               |
| `RELAY_DATA_HOST`                       | relay        | no     | Interface for data-relay sockets (`0.0.0.0`)                           |
| `APPROVAL_DEMO_TOKEN`                   | admin        | yes    | Shared token for the approval service's write routes                   |

The names of the approval service's other variables and of `agent-mcp/` are Open: those packages are not on `hackathon` yet.

The relay generates its token secret itself at each start (`relay/sidecar.go`), so no relay secret exists to store.

## NAT box

| Item           | Value                                                    |
| -------------- | -------------------------------------------------------- |
| Namespace      | `bramble-node` (override with `NATBOX_NS`)               |
| Host side      | `bnb-host`, `10.200.0.1/24`                              |
| Node side      | `bnb-node`, `10.200.0.2/24`                              |
| nftables table | `ip bramble_natbox`                                      |
| Modes          | `cone` (`masquerade`), `symmetric` (`masquerade random`) |
| State files    | `/run/bramble_natbox.mode`, `/run/bramble_natbox.block`  |
