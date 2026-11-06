# Why the demo topology looks this way

This page explains the choices behind `README.md`. The handoff (section 5.3) fixes three cloud machines plus an admin machine. The choices below fill the gaps. They are recorded, with the alternatives, in `docs/adr/0015-cloud-demo-topology.md` (draft, awaiting human approval).

## Decisions

| #   | Decision                                                                                                        | Reason                                                                                                                                    |
| --- | --------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | The RPC proxy runs on the relays machine.                                                                       | It needs a public HTTPS name. That machine is already public. It keeps the count at three machines.                                       |
| 2   | Nodes use the proxy URL as `SEPOLIA_RPC_URL`. Only the proxy and the admin machine hold the dedicated endpoint. | A node file then holds no credentialed URL. Beat 6 needs nodes to call the proxy anyway.                                                  |
| 3   | The admin machine uses the dedicated endpoint directly.                                                         | Beat 6 compares node traffic before and after Tor. Admin traffic would add noise to the proxy log.                                        |
| 4   | The public console URL comes from a reverse SSH tunnel to Caddy on the relays machine.                          | It adds no third-party tunnel service and no second domain. The cost: the admin machine must stay online.                                 |
| 5   | Each of gateway and worker is its own NAT through a network namespace on the same machine (NAT box).            | Beat 4 needs two nodes behind different NATs. NAT router machines would raise the count to five.                                          |
| 6   | The STUN server is coturn in STUN-only mode on the relays machine.                                              | The preflight needs a known STUN target. A public STUN server would see demo node addresses it has no need to see.                        |
| 7   | Relay units are a systemd template, `bramble-relay@<instance>`.                                                 | The handoff says "relays". One env file per instance supports one relay or two (a second relay lets a node show failover between relays). |
| 8   | Node units run as an unprivileged user with `CAP_NET_ADMIN`.                                                    | Least privilege. A root drop-in is the fallback because the capability setup has not run.                                                 |
| 9   | The agent runs on the worker machine as its own Unix user.                                                      | The forward listens on the worker's loopback. The agent user cannot read the node's keys.                                                 |
| 10  | In NAT-box mode `demo-service` joins the gateway's namespace, and the agent starts through `agent-in-box.sh`.   | Loopback is per namespace: the gateway dials demo-service on its own loopback, and the agent dials the forward on the worker's.           |

## What each NAT mode models

`cone` uses Linux source NAT, which keeps the source port when it is free and drops unsolicited inbound packets. That resembles a port-restricted cone NAT, and hole punching can work through it. `symmetric` randomises the source port per flow. A peer cannot predict the mapped port, so direct paths usually fail and the node must fall back to the relay. The first claim is Read from source (Linux netfilter behaviour); neither mode has been measured with a STUN client yet.

## What stays honest

- A single NAT-box machine has a public address that the cloud provider also translates (one-to-one NAT). The path is double NAT. The demo claims "different NATs", which holds, and does not claim a consumer router.
- STUN and the relays still see node IP addresses. The Tor upgrade covers the control plane only (handoff section 4, "Questions to prepare").
- The admin machine remains a single trusted operator host. The approval service writes chain state; peers still enforce access themselves.
