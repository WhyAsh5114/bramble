# ADR 0020 — Relay rediscovery while a node runs

**Status:** Decided 9 Nov 2026. Extends ADR 0003 and the relay directory of ADR 0019 (user-driven tailnets).

## Context

`serve` read the relay directory once, at startup. A node started before any relay was listed got no rendezvous relay, no data relay and no STUN server, and kept running that way until restarted. Only the rendezvous set had a retry, and that retry ran only while the set was empty. It also threw away the data relays and STUN servers in the same answer.

Because the STUN server list was empty at startup, the node turned STUN off. That also disabled hole punching for the life of the process.

## Decision

- `serve` keeps one live `relayDirectory`. A background loop refreshes it every 15 seconds (`relayRefreshInterval`). Every consumer reads it on each use: the endpoint resolver, traversal, the data-relay failover and adopt watchers, `RunSTUN` and the status API's `/relays`.
- Each refresh runs `chooseRelays` and merges the result. The node never takes the raw directory.
- A refresh **adds relays and never removes one**. A live session may be using a relay, and `relayHealth` already steers new work away from one that fails. Each set is capped at the size `chooseRelays` returns: 4 rendezvous relays, 5 data relays, 4 STUN servers.
- Flags fix their own set. `-rendezvous`, `-data-relay` and `-stun-servers` or `-no-stun` each stop the directory from changing that set. When both `-rendezvous` and `-data-relay` are given, the directory is not read at all, and STUN stays off unless `-stun-servers` is set.
- `serve` decides once, from flags, whether STUN is on: it is off with `-no-stun`, or when every relay set came from flags and no `-stun-servers` was given. Only the server list is read live. A peer therefore never switches between traversal and the static resolver while the node runs. While no STUN server is known, the endpoint status is `unavailable` with "no STUN servers known yet".

## Consequences

- A node started before its first relay existed starts using that relay on the first refresh after the registry cache sees it, so within one `relayRefreshInterval` (15 seconds). It then gets a STUN endpoint on the next `RunSTUN` pass, at most 30 seconds later. Both bounds come from the intervals and are untested at these values. On 9 Nov 2026, `TestRelayListedAfterStartIsUsedWithoutRestart` (refresh shortened to 200 ms, netstack on one Mac) connected two nodes 11 s after the relay was listed.
- A node that reads the directory now always takes the traversal path, even with no STUN server yet. Until one appears, traversal announces only the static candidate and the relay candidate. Before this change, such a node used the static resolver and never punched.
- Removing a relay from the directory does not remove it from running nodes. A restart drops it.
