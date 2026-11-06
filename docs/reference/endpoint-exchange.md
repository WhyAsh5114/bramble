# Endpoint exchange message format

Reference for how two Bramble nodes swap their UDP endpoints through the paid rendezvous channel (W8b). Code: `brambled/rendezvous/endpoints.go`. Tests: `brambled/rendezvous/endpoints_test.go`, `brambled/rendezvous/endpoints_integration_test.go`.

## Rules

1. Endpoints travel only through the rendezvous relay (`relay/`, metered by [ADR 0007](../adr/0007-rendezvous-relay-metering.md)). They are never written on-chain (invariant 2).
2. A learned endpoint is a dial hint. It never grants admission (invariant 1). `admission.Loop` calls `EndpointResolver` only for peers ENS currently admits. `Exchanger.Learn` adds a second check, and STUN-enabled automatic traversal in `brambled serve` uses it. The data-relay adopt watcher in `brambled/main.go` (`watchDataRelayAdopt`) predates this format and does not check admission; gating it is a follow-up (Open).
3. The relay stays opaque. It forwards the `candidate` string unchanged and does not parse it.

## Carrier

The announcement rides in the existing `candidate` field of the relay's `offer` message. The relay wire protocol does not change.

```json
{"type":"offer","to":"<peer pubkey hex>","candidate":"<announcement>"}
```

## Announcement grammar

```
announcement = "ep1:" entry *("," entry)
entry        = kind "=" ip-port
kind         = "static" / "srflx" / "host" / "relay"
ip-port      = IPv4 ":" port / "[" IPv6 "]" ":" port
```

Example:

```
ep1:srflx=203.0.113.7:41000,host=192.168.1.5:51820
```

| Kind     | Meaning                                                           | Source                  |
| -------- | ----------------------------------------------------------------- | ----------------------- |
| `static` | Address an operator chose                                         | `-my-candidate`         |
| `srflx`  | Server-reflexive address a STUN server reported (RFC 5389)        | W8a STUN client         |
| `host`   | Local interface address; reachable only on a shared network       | Local interface scan    |
| `relay` | Shared port-per-session data-relay address | Elected buyer |

## Legacy candidates

A candidate with no `ep1:` prefix is a bare `ip:port`. The receiver decodes it as one `static` endpoint. The empty string means the peer published no address.

## Limits

| Limit                       | Value |
| --------------------------- | ----- |
| Entries per announcement    | 8     |
| Announcement length (bytes) | 512   |

## Receiver behavior

| Input                                                                        | Result                                       |
| ---------------------------------------------------------------------------- | -------------------------------------------- |
| Entry with an unknown `kind` and a valid address                             | Skipped, so new kinds can be added later     |
| Entry that is not `kind=ip:port`, or has a bad address                       | The whole announcement is rejected           |
| Port 0, unspecified (`0.0.0.0`, `::`), multicast, link-local, or broadcast (`255.255.255.255`) address, including the IPv4-mapped forms | The whole announcement is rejected |
| Address with an IPv6 zone (`%...`)                                           | The whole announcement is rejected. The relay is untrusted and a zone can carry a newline into the WireGuard UAPI line. |
| Duplicate address                                                            | First occurrence kept                        |
| Loopback address                                                             | Dropped by `Exchanger.Learn` unless `AllowLoopback` is set (tests only) |
| More than 8 entries, or over 512 bytes                                       | The whole announcement is rejected           |

`Announcement.Best()` returns one endpoint for callers that can try only one. The order is `static`, then `srflx`, then `host`, then `relay`. Automatic traversal separates the relay hint and tries the preferred direct candidate. Only the lower public key buys a relay session; the other side learns that shared address.

## Admission gate

`Exchanger.Learn(peerPubkey)` enforces rule 2:

1. It calls `Admitted(peerPubkey)` before any network activity. If the result is false, or `Admitted` is nil, it returns `ErrNotAdmitted`. The node publishes nothing to that peer and pays for no rendezvous token.
2. It runs the exchange.
3. It calls `Admitted` again. ENS can revoke the peer while the exchange waits. A revoked peer's endpoints are discarded and `Learn` returns `ErrNotAdmitted`.
4. It parses the peer's announcement and returns the usable endpoints.

`admission.Loop` already calls its `EndpointResolver` only for peers it has authorized. `Admitted` is a second check, so a wiring mistake cannot leak endpoints to, or accept them from, an unadmitted key.

## What the relay and peers learn

- The relay sees each node's public key, the peer's public key, and the announced endpoints, in plaintext. Announced endpoints are public addresses by design.
- A STUN server sees the address of whoever asks. This does not go through Tor.
- The relay can forge or replay an announcement. A forged endpoint can only misdirect a probe or a handshake. WireGuard still completes a handshake only with a key in the peer table, and ENS alone fills that table.

## Status

| Claim                                                                     | Label                                      |
| ------------------------------------------------------------------------- | ------------------------------------------ |
| Format, parser, and admission gate behave as described above              | Verified by `go test ./rendezvous/` on 6 Nov 2026 |
| Two in-process nodes learn each other's endpoint over the relay protocol and handshake | Verified (`TestEndpointsLearnedOverRendezvousReachAdmittedPeersOnly`, loopback, netstack) |
| `brambled serve` publishes `ep1:` announcements with a STUN endpoint      | Read from source: STUN-enabled automatic traversal publishes its current observation |
| `brambled serve` uses `Exchanger` (and its admission gate) | Read from source: automatic traversal uses `Exchanger` after admission |
| `brambled serve` accepts both formats from a peer                         | Verified for the parser (`TestPeerEndpointFromCandidate`); not run against a live relay |
| Works across two real NATs                                                | Open                                       |
