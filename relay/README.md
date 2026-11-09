# relay

A free, permissionless Bramble relay ([ADR 0019](../docs/adr/0019-user-driven-tailnets.md)). It offers three things:

- **Rendezvous** (TCP 9420, TLS): peers exchange sealed connection candidates. The relay routes them by pseudonymous IDs and cannot read or forge them.
- **Data relay** (UDP 40000–40999): for peers that cannot hole-punch, a per-session UDP port that forwards WireGuard ciphertext between the two peers that proved the session secret.
- **STUN** (UDP 3478): tells a node its public address.

The relay never decides who may talk to whom. Each peer checks admission against the chain itself.

## Run one

With the `bramble` binary (see the top-level README for the installer):

```sh
bramble relay -public-host <your public IP>
```

Or as a container built from this directory (the compose file in `deploy/projects-host/relay/` runs it under gVisor):

```sh
docker build -t bramble-relay relay
docker run -d --name bramble-relay \
  -p 9420:9420/tcp -p 3478:3478/udp -p 40000-40999:40000-40999/udp \
  -e RELAY_PUBLIC_HOST=<your public IP> -v bramble-relay:/state bramble-relay
```

It logs its TLS pin on start (`bramble relay pin` or `docker exec bramble-relay /relay pin` prints it again). List the relay in the directory from the console's **Relays** page with that pin.

## Configuration

| Variable                           | Default          | Meaning                                            |
| ---------------------------------- | ---------------- | -------------------------------------------------- |
| `RELAY_PUBLIC_HOST`                | (required)       | Public IP or DNS name peers reach data sessions at |
| `RELAY_ADDR`                       | `:9420`          | Rendezvous listener (TLS)                          |
| `RELAY_STUN_ADDR`                  | `:3478`          | STUN listener; empty disables it                   |
| `RELAY_PORT_MIN`, `RELAY_PORT_MAX` | `40000`, `40999` | UDP ports for data sessions                        |
| `RELAY_MAX_BYTES_PER_DAY`          | 3 GiB            | Data relay budget per UTC day; `0` is unlimited    |
| `RELAY_MAX_BYTES_PER_MONTH`        | 80 GiB           | Data relay budget per UTC month; `0` is unlimited  |
| `RELAY_SESSION_BYTES`              | 512 MiB          | Cap of one session                                 |
| `RELAY_SESSION_TTL_SECONDS`        | `1800`           | Lifetime of one session                            |
| `RELAY_MAX_SESSIONS`               | `256`            | Concurrent sessions                                |
| `RELAY_SESSIONS_PER_IP_PER_HOUR`   | `60`             | New sessions one source IP may open per hour       |
| `RELAY_RENDEZVOUS_ONLY`            | unset            | `1` turns the data relay off                       |
| `RELAY_STATE_DIR`                  | `/state`         | TLS key and byte counters                          |

When the day or month budget is used up, the relay stops forwarding data until the period rolls over. Rendezvous and STUN keep working, so peers that can hole-punch still connect.

## Paid data sessions (opt-in, Hedera testnet)

Set `RELAY_X402_PAY_TO` to charge for data sessions. Rendezvous and STUN stay free. The relay then answers `POST /x402/session` on the same TLS port with `402 Payment Required`. A node pays with the x402 `exact` scheme on Hedera testnet, the facilitator verifies and settles, and the relay returns a single-use token that the node presents in the rendezvous `session` message. See [ADR 0021](../docs/adr/0021-paid-relays-and-audit-trail.md).

| Variable                   | Default                             | Meaning                                                  |
| -------------------------- | ----------------------------------- | -------------------------------------------------------- |
| `RELAY_X402_PAY_TO`        | unset                               | Hedera account that receives payment; turns paid mode on |
| `RELAY_X402_PRICE_PER_MIB` | `0` (required)                      | Price of 1 MiB in the asset's smallest unit              |
| `RELAY_X402_ASSET`         | `0.0.429274`                        | Token paid in (testnet USDC)                             |
| `RELAY_X402_NETWORK`       | `hedera:testnet`                    | Only `hedera:testnet` is accepted                        |
| `RELAY_X402_FACILITATOR`   | `https://api.testnet.blocky402.com` | Facilitator base URL                                     |
| `RELAY_X402_PUBLIC_URL`    | unset                               | `https` URL of this relay, named in 402 responses        |

The relay builds the payment requirements itself and sends only those to the facilitator. It issues a token only after settlement succeeds. A token works once and expires after 10 minutes. The token secret is saved in `RELAY_STATE_DIR`, so a restart does not void a token that was already paid for. A used-token list is kept in memory, so a restart inside the 10 minutes would accept a spent token again.

### Audit log (opt-in)

Set `RELAY_HEDERA_ACCOUNT` and `RELAY_HEDERA_KEY` (a Hedera testnet account with a little HBAR) and the relay writes a receipt to an HCS topic for each session it sells. With no `RELAY_HCS_TOPIC` it creates a topic that only its account can write to and remembers it in the state directory. The status reply names the topic. A receipt holds the payment transaction, byte count, amount, asset and time, and no payer or peer.

**Verified:** these behaviors against a fake facilitator in `relay/relaysrv/x402_test.go`, and one settled HBAR payment against the live Blocky402 testnet facilitator on 10 Nov 2026. **Open:** a payment in USDC.

## What a relay operator can and cannot do

- **Sees:** source IP addresses, timing and byte counts of its users, and which pseudonymous IDs talk to each other.
- **Cannot:** read candidates or traffic, learn WireGuard keys, impersonate a peer or admit a device.
- **Can:** drop or delay traffic. Clients use several relays and prefer direct paths, so a bad relay slows a connection down without breaking it.
