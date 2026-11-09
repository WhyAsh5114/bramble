# relay

A free, permissionless Bramble relay ([ADR 0019](../docs/adr/0019-user-driven-tailnets.md)). It offers three things:

- **Rendezvous** (TCP 9420, TLS): peers exchange sealed connection candidates. The relay routes them by pseudonymous IDs and cannot read or forge them.
- **Data relay** (UDP 40000–40999): for peers that cannot hole-punch, a per-session UDP port that forwards WireGuard ciphertext between the two peers that proved the session secret.
- **STUN** (UDP 3478): tells a node its public address.

The relay never decides who may talk to whom. Each peer checks admission against the chain itself.

## Run one

```sh
docker run -d --name bramble-relay \
  -p 9420:9420/tcp -p 3478:3478/udp -p 40000-40999:40000-40999/udp \
  -e RELAY_PUBLIC_HOST=<your public IP or DNS name> \
  -v bramble-relay:/state \
  ghcr.io/whyash5114/bramble-relay:latest
```

Print the TLS pin that clients check, then list the relay in the directory from the Bramble UI (**Relays → Publish a relay**):

```sh
docker exec bramble-relay /relay pin
```

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

## What a relay operator can and cannot do

- **Sees:** source IP addresses, timing and byte counts of its users, and which pseudonymous IDs talk to each other.
- **Cannot:** read candidates or traffic, learn WireGuard keys, impersonate a peer or admit a device.
- **Can:** drop or delay traffic. Clients use several relays and prefer direct paths, so a bad relay slows a connection down without breaking it.
