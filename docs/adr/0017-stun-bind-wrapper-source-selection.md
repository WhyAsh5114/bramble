# ADR 0017: STUN discovery wraps WireGuard's bind and gives up source stickiness

**Status:** Draft, 6 November 2026. Human approver: pending.
**Workstream:** W8a (STUN endpoint discovery), demo beat 4. See `docs/eth-36-stun-spec.md` for the protocol reference.

## Context

Discovery must observe the public endpoint of the UDP socket WireGuard uses, so the STUN client cannot open its own socket. wireguard-go's receive goroutines are the only readers of that socket, so STUN replies have to be consumed from their receive path.

wireguard-go's `conn.Bind` is an interface, so `brambled/stun` wraps it. `*conn.StdNetBind` caches the local source address and interface on each peer endpoint and, on Linux, installs a route-change listener that invalidates that cache. The listener is only installed when the bind is a concrete `*conn.StdNetBind`. A wrapper never gets it, so reusing the cached source would pin traffic to a stale interface after a route change.

wireguard-go also sends handshake and data packets for one peer from different goroutines against the same `conn.Endpoint`, and releases its endpoint lock before calling `Bind.Send`. An endpoint passed to `Send` is therefore shared and must not be mutated there. The first implementation called `ep.ClearSrc()` in `Send` and raced with the inner bind's reads of the cached source (reported in review ETH-89 and reproduced under `-race`).

## Decision

1. **Wrap the default bind only when STUN is enabled.** `-stun-servers` or `BRAMBLE_STUN_SERVERS` opts in. With it unset, the node keeps the unwrapped default bind and its route-change handling.
2. **In the wrapper, `Send` forwards a fresh destination-only endpoint** built with `ParseEndpoint(ep.DstToString())`. The inner bind then has no cached source and the kernel selects the current route and source on every write. The caller's endpoint is never modified.
3. **Replies are matched by transaction ID and server address** in the receive path. The wrapper zeroes the size of STUN packet slots and leaves WireGuard packets unchanged.

## Consequences

- With STUN enabled, WireGuard's source-address stickiness is gone. On a multihomed host the kernel may pick a different source address between packets (not tested). Real multihomed and NAT route changes are **Open**: they have not been tested.
- Each send allocates one small endpoint. This is not measured; the cost is expected to be small next to the syscall.
- The discovered endpoint is an observation from one STUN server, not proof of peer reachability. The STUN server learns the asker's public IP and port, so UDP STUN cannot be used over Tor. This is disclosed in the README CROPS (P) line.

## Rejected alternatives

- **A second UDP socket for STUN.** It observes a different NAT mapping from WireGuard's socket, which defeats the purpose.
- **Calling `ClearSrc()` on the shared endpoint.** It races with concurrent sends (above).
- **Copying wireguard-go's route-change listener into the wrapper.** It duplicates unexported dependency behavior that would need tracking on every wireguard-go bump.
- **Always wrapping, regardless of the flag.** It would remove source stickiness from nodes that do not use STUN.
