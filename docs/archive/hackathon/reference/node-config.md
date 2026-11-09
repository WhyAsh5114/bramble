# Node config (`/.well-known/bramble.json`)

Reference for the public config a device fetches before it joins a tailnet. `bramble up` reads it from `https://<install-host>/.well-known/bramble.json` (or any URL or file given with `--config`). It holds public values only: no keys, no tokens. The URL layout is the ETH-177 shared contract v1; how the install host fills the file is in `docs/reference/install-host.md`.

Code: `node-daemon/internal/daemon/config.go` (parse and validate), `node-daemon/internal/daemon/node.go` (turns it into the `brambled serve` command line and environment).

Unknown fields are ignored. Wrong values do not stop `bramble up`: it keeps serving the local UI, and the first step that needs the config (Join, or starting the node) shows an error with a next step. `bramble` fetches the file again at each of those steps, so fixing it on the install host is enough.

## Required

| Field | Type | Meaning |
|---|---|---|
| `tailnet` | string | ENS name of the tailnet. Lower-cased. |
| `registry` | `0x` address | Tailnet registry contract. Also accepted as `registries.tailnet`. |
| `joinApi` | http(s) URL | Base URL of the approval service. The device calls `<joinApi>/api/joins`. No credentials in the URL. |

## Optional

| Field | Type | Default | Meaning |
|---|---|---|---|
| `version` | string | | Release the config belongs to. |
| `ensDeployment` | `hackathon-test` or `sepolia-v2` | `hackathon-test` | Selects the ENS contract set. Becomes `BRAMBLE_ENS_DEPLOYMENT`; the daemon supplies the compatibility default even if its parent environment selects another deployment. |
| `chainId` | number | | Chain id (Sepolia is 11155111). Informational. |
| `rpcUrl` | http(s) URL | brambled default | Becomes `SEPOLIA_RPC_URL` for the node. Use the logging proxy, never a credentialed endpoint. |
| `relayRegistry` | `0x` address | | Relay registry. Also `registries.relay`. |
| `registryDeployBlock`, `relayDeployBlock` | number | 0 | Where the sidecar starts its log scans. The RPC must serve logs back to these blocks. |
| `rendezvous` | `host:port` | | Rendezvous relay, passed as `-rendezvous`. |
| `stun` | string or list | | STUN servers `host:port`, passed as `-stun-servers`. |
| `dataRelays` | map label to URL | | Data relay pool, one `-data-relay label=url` each. |
| `relayPeers` | list of labels | | Peers routed through the data relay pool, one `-relay-peer` each. Ignored when `relayFirst` is on. |
| `peers` | list of labels | all devices | Limits the device to these peers (ADR 0010). Service and relay peers are always added. Labels only, never endpoints. |
| `services` | list of `{peer, service, path?}` | | Services the daemon forwards to loopback so `POST /api/test` can reach them through the mesh, and the local UI can open them (`GET /api/state` `services[].url`). `path` defaults to `/`. |
| `sponsoredRelay` | bool | `true` | Pass `-sponsored-relay`: the relays grant operator-sponsored sessions, so a device with no Hedera key pays nothing (ADR 0018). |
| `relayFirst` | bool | `true` when `dataRelays` is not empty | Pass `-relay-first`: every peer goes through the data relay pool, so the device works with no direct path. Hole punching (ETH-39) upgrades it later. |
| `rendezvousPaymentUrl` | http(s) URL | | Payment sidecar paired with `rendezvous`. Becomes env `BRAMBLE_RENDEZVOUS_PAYMENT_URL`. Required when the relay runs `-meter`: without it the relay rejects every rendezvous hello and no peer gets an endpoint. `gen-node-config.mjs` sets it from `BRAMBLE_RENDEZVOUS_PAYMENT_URL`, or else from the data relay on the rendezvous host. |

## What the daemon passes to `brambled serve`

For a device with no Hedera key:

- `-label <own-label> -netstack -keepalive 25` plus the status port and bind.
- `-sponsored-relay` unless `sponsoredRelay` is false.
- `-relay-first`, or `-relay-peer <label>` per `relayPeers` entry when `relayFirst` is false.
- `-data-relay label=url` per `dataRelays` entry, `-rendezvous`, `-stun-servers`.
- `-forward <port>=<peer>:<service>` per `services` entry.
- Never `-peer <endpoint>` and never a Hedera key. The device key goes in `BRAMBLE_PRIVATE_KEY`, never on the command line.

The flags `-sponsored-relay` and `-relay-first` come from PR #35 (ETH-189). Until that merges, a `brambled` without them exits at startup with an unknown-flag error, which `bramble` reports as an error state.

## Example

```json
{
  "version": "0.1.0",
  "tailnet": "demo.eth",
  "chainId": 11155111,
  "rpcUrl": "https://rpc.example.test/sepolia",
  "registries": {"tailnet": "0x1111111111111111111111111111111111111111", "relay": "0x2222222222222222222222222222222222222222"},
  "joinApi": "https://console.example.test",
  "rendezvous": "203.0.113.1:7000",
  "stun": "203.0.113.1:3478",
  "dataRelays": {"relay-a": "http://203.0.113.1:7891"},
  "relayPeers": ["gateway"],
  "services": [{"peer": "gateway", "service": "health"}],
  "rendezvousPaymentUrl": "http://203.0.113.1:7892"
}
```
