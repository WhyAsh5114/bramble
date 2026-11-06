# Demo launcher reference

Facts for the commands in [README.md](README.md).

| Term    | Meaning                                                                    |
| ------- | -------------------------------------------------------------------------- |
| NAT     | Network address translation. The router behaviour the NAT box simulates.   |
| STUN    | Session Traversal Utilities for NAT. How a node learns its public address. |
| TCP     | Transmission Control Protocol. The transport relays accept connections on. |
| API     | Application programming interface.                                         |
| ENS     | Ethereum Name Service.                                                     |
| DER     | Distinguished Encoding Rules. The binary encoding of a Hedera key.         |
| ECDSA   | Elliptic Curve Digital Signature Algorithm. One Hedera signature scheme.   |
| Ed25519 | Edwards-curve signature scheme over Curve25519. The other Hedera scheme.   |

Evidence labels are the ones in `docs/15_MUMBAI_HANDOFF.md`: **Verified**, **Read from source**, **Open**. Anything run against real machines is **needs human verification**.

## Roles and units

| Role      | Machine        | Units, in start order                                          |
| --------- | -------------- | -------------------------------------------------------------- |
| `relays`  | Relays machine | `bramble-relay@<instance>` for each instance                   |
| `gateway` | Gateway node   | `[bramble-nat-box]`, `bramble-demo-service`, `bramble-gateway` |
| `worker`  | Worker node    | `[bramble-nat-box]`, `bramble-worker`                          |
| `admin`   | Admin machine  | `bramble-approval`, `bramble-console-tunnel`                   |

Unit names are **Read from source** (`deploy/REFERENCE.md` from ETH-49, not merged into `hackathon` at the time of writing). The relays machine also needs `rpc-proxy` (unit file from ETH-44, unmerged), `caddy` and `coturn`. The launcher does not start them, because the installer does not enable them yet. It prints `sudo systemctl enable --now rpc-proxy caddy coturn`, and the preflight check `services:relays` fails on the relays machine until all three are active. Whether those three units exist on a real machine is **needs human verification**.

## Connection settings

Set these in the repo `.env` or the shell. The launcher and preflight print only their names.

| Variable                                  | Meaning                                                                                       |
| ----------------------------------------- | --------------------------------------------------------------------------------------------- |
| `DEMO_GATEWAY_SSH`, `DEMO_WORKER_SSH`     | ssh target (`user@host` or a Host alias) for a node this machine does not run                 |
| `DEMO_GATEWAY_NETNS`, `DEMO_WORKER_NETNS` | Network namespace of a remote node inside the NAT box                                         |
| `DEMO_NAT_BOX`                            | `1` when this machine's nodes run in the NAT box: starts `bramble-nat-box`, probes inside it  |
| `NATBOX_NS`                               | NAT box namespace name, default `bramble-node`                                                |
| `DEMO_RELAY_INSTANCES`                    | Comma list of relay instance names, default from `/etc/bramble/relay-*.env`                   |
| `DEMO_AGENT_LABEL`                        | Agent device label. Also checked against the device registry                                  |
| `DEMO_GRANTER_LABEL`                      | Granter device label (reset)                                                                  |
| `DEMO_PAYER_ACCOUNT_IDS`                  | Comma list of Hedera accounts to check. Default: each local node's `HEDERA_CLIENT_ACCOUNT_ID` |
| `DEMO_MIN_PAYER_USDC_ATOMIC`              | Minimum payer balance in atomic USDC, default `500000` (0.5 USDC)                             |
| `HEDERA_MIRROR_URL`                       | Mirror node base URL, default the public testnet mirror                                       |

The sidecar (port 7890) and the node status API (port 7899) bind to loopback, so the preflight reaches another machine's node with `ssh <target> curl http://127.0.0.1:<port>/...`, wrapped in `sudo -n ip netns exec <ns>` for the NAT box. ssh runs with `BatchMode=yes` and `StrictHostKeyChecking=yes`, so the host key must already be known. A node the preflight cannot reach is a `FAIL`, never a skip.

## Preflight checks

| Check id          | What it verifies                                                                                                                                                             | Status of the endpoint it reads                                         |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `env:<role>`      | Required variable **names** are set for each local role. Values are never printed. Admin also needs the granter key (env or Key Ring file) and a `HEDERA_SERVER_*` variable  | Names: Read from source. The exact `HEDERA_SERVER_*` names are **Open** |
| `sepolia-rpc`     | `SEPOLIA_RPC_URL` is set (value hidden) → `PASS`. Unset → `WARN` that reads may be stale                                                                                     | Verified (unit test)                                                    |
| `sidecar:<node>`  | `GET /health` is `ok` and serves the expected tailnet                                                                                                                        | Read from source                                                        |
| `tor:<node>`      | `GET /tor/status` reports `ready`                                                                                                                                            | Read from source (ETH-59, unmerged)                                     |
| `stun:<node>`     | `GET /endpoint` on the status API reports `discovered`                                                                                                                       | Read from source (ETH-36, merged into `hackathon`)                      |
| `registry:<node>` | `GET /devices` resolves and lists the agent device                                                                                                                           | Read from source                                                        |
| `relay`           | At least one rendezvous relay accepts a TCP connection **and** at least one payment sidecar answers `/health`, or it fails. A down redundant relay is a `WARN` that names it | Read from source                                                        |
| `services:relays` | On the relays machine only: `rpc-proxy`, `caddy` and `coturn` are active (`systemctl is-active`)                                                                             | Read from source (ETH-49, ETH-44, unmerged)                             |
| `hedera:<id>`     | The payer account is associated with USDC `0.0.429274` and holds at least the minimum, from the mirror node                                                                  | Read from source                                                        |

`pnpm demo` and `pnpm demo:preflight` are **Verified** only as far as the unit tests and a local dry run: no real run has happened. A real run needs the machines, so it is **needs human verification**.

## Secrets

The scripts never print an env value, an RPC URL, or a key. Every check detail and every error is scrubbed before it is shown: URLs (to the end of the token), runs of 64 or more hex digits and Hedera DER keys (Ed25519 and ECDSA, with or without `0x`), and every value of a secret-named variable, whatever its length (`*KEY*`, `*TOKEN*`, `*SECRET*`, `*PASSWORD*`, `*RPC_URL*`, `*_PK`) in the process env, the repo `.env` and `/etc/bramble/*.env`. Text that a remote node reports (Tor, STUN and registry errors, including an unrecognised STUN state) is replaced by a fixed message. A check id never contains an unvalidated value: an invalid payer account is reported as `hedera:invalid-<n>`. Output of the commands the launcher runs is read and dropped, not forwarded. The reset prints transaction hashes and ENS names only.
