# bramble

A WireGuard mesh with no coordination server. A tailnet's membership lives in an ENS registry that its admin's own
wallet owns on Sepolia. Every device follows that registry itself, finds its peers through relays that only see sealed
messages, and connects directly when NAT allows or through a relay when it does not.

```sh
curl -fsSL https://get-bramble.whyash5114.com/install.sh | sh
```

One `bramble` binary for Linux and macOS (amd64, arm64). It runs the node and serves a console on `127.0.0.1:7777`
where you create a tailnet, join one, enroll devices, grant access and share services.

> Testnet only. bramble is a portfolio project, not audited software.

## How it works

| Piece               | What it does                                                                                                                                                                                                                                           |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `net.bramble.eth`   | Open first-come registrar ([`contracts/`](contracts)). Claiming `alice` deploys a registry and resolver the claimer administers. Devices are labels in it; their records hold a WireGuard key, a mesh IP, revocation and ACL digests.                  |
| `relay.bramble.eth` | Open relay directory. Anyone can claim a name and publish a rendezvous address and a TLS pin.                                                                                                                                                          |
| Registry cache      | Each device replays its tailnet's registry events into an on-disk cache and follows new blocks across several public RPCs ([`brambled/registry`](brambled/registry)). Revocations reach peers within seconds.                                          |
| Rendezvous          | Two devices derive a shared key from their WireGuard keys and exchange endpoint candidates sealed with it, so a relay sees pseudonymous IDs and ciphertext only ([`brambled/rendezvous`](brambled/rendezvous)).                                        |
| Data relay          | When hole punching fails, a relay forwards WireGuard packets inside a session bound by a secret only the two peers know. It carries ciphertext and enforces daily, monthly and per-session byte budgets ([`relay/`](relay)).                           |
| Relay selection     | Devices use the tailnet's own `relays` record alone when any of it is usable, otherwise the three oldest directory entries; they skip paid or unpinned relays and drop relays that stop answering ([ADR 0019](docs/adr/0019-user-driven-tailnets.md)). |
| Gateway             | Each device exposes its services over the tunnel. Open services reach every member; private ones need a grant that a trusted granter computes and the admin writes on-chain ([ADR 0005](docs/adr/0005-acl-record-schema.md)).                          |
| Console             | Next.js static export embedded in the binary ([`local-ui/`](local-ui)). Writes go through the browser wallet; the daemon never holds a chain key.                                                                                                      |

### Create a tailnet

1. Run `bramble up` and open the console. Connect a browser wallet holding a little Sepolia ETH.
2. Choose a name and this device's name. The wallet signs three transactions: claim, register the device, write its
   records. In testing this took 36 seconds from click to connected.

### Add a device

1. Install bramble on it, choose **Join a tailnet** and enter the name. It shows a join code and a key fingerprint.
2. On the admin's console, paste the code under **Devices → Enroll**, compare the fingerprint, sign.

### Run a relay

```sh
bramble relay -public-host <public IP>   # 9420/tcp rendezvous, 3478/udp STUN, 40000-40999/udp data
```

Publish it from the console's **Relays** page with the pin it prints. See [`relay/README.md`](relay/README.md) for the
budget settings.

## Repository

| Path                                | Contents                                                                   |
| ----------------------------------- | -------------------------------------------------------------------------- |
| `node-daemon/`                      | The `bramble` binary: daemon, local API, CLI, embedded console             |
| `brambled/`                         | The node: WireGuard, registry cache, rendezvous, gateway, relay client     |
| `relay/`                            | Rendezvous, STUN and data relay server                                     |
| `local-ui/`                         | The console                                                                |
| `contracts/`                        | `OpenRegistrar` and its Sepolia deployment                                 |
| `landing/`, `deploy/projects-host/` | get-bramble.whyash5114.com and the host that serves it and the relay       |
| `docs/adr/`                         | Design decisions                                                           |
| `docs/archive/hackathon/`           | Planning and demo material from ETHGlobal Mumbai (tag `archive/hackathon`) |

## Develop

```sh
pnpm install
pnpm verify                      # format, checks, typecheck, lint, build, tests
cd brambled && go test ./...     # node tests, including two-node end to end
VERSION=v0.x.y scripts/release.sh
```

`pnpm --filter @bramble/local-ui dev` serves the console with `/api` proxied to a running `bramble up`
(`BRAMBLE_API`, default `http://127.0.0.1:7777`). Set `BRAMBLE_TRACE=1` on a node or `RELAY_TRACE=1` on a relay to log
each step of discovery, admission and relaying.

## License

See [LICENSE](LICENSE).
