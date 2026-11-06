# Resolver optimization reference

`BRAMBLE_FAST_RESOLVE=1` enables the resolver optimization in `brambled` and its child sidecar. An unset variable or any other value preserves the sequential device and gateway reads. The default is off. The child inherits the variable from the node environment.

| Operation | Flag off | Flag on |
| --- | --- | --- |
| Device resolution | Seven sequential ENS reads | One Sepolia Multicall3 `aggregate3` call |
| Multicall failure, subcall failure or malformed response | Not applicable | Fresh individual reads in parallel through the same client |
| Gateway decision | Own record, requester, then granters in order | Own record and requester in parallel; then trusted granters in parallel; return on the first completed matching grant |
| Startup | Existing discovery and mesh IP reads | Also resolve own device, known peers (including forward gateways), and own trusted granters before listeners start |
| Routing metadata | No new cache | In-memory cache for `mesh-ip`, `rendezvous-address` and `sidecar-url`; 60-second TTL; at most 1,024 entries per client |

The cold batch contains five Universal Resolver text lookups, `findResolver`, and the tailnet registry's `getState`. The text lookups use viem 2.56's `resolveWithGateways` ABI on the hackathon Universal Resolver. A warm routing cache omits the mesh IP subcall. Absent records and failed reads do not enter the cache. Cache keys include the node's client, registry, full name and text key.

ACL digests, `acl-granters`, `revoked`, registry status, expiry, token ID, resolver address and pubkeys remain fresh. Clearing a pubkey is a revocation mechanism, so pubkeys have no TTL. Every gateway decision and admission check still reads authorization from ENS. A failed fresh read returns an error; a cached routing record cannot authorize access. Existing admission polling and connection shutdown behavior apply.

Multicall3 address: `0xcA11bde05977b3631167028862bE2a173976CA11` on Sepolia. Deployment verification on the dedicated RPC is **Open**, owned by Infra-Sol-UI under ETH-244. The cloud environment files remain unchanged. Temporary flag-on measurement runs and a persistent flag change belong to the coordinated cloud slot; persistent enablement follows the measurements.

## Provider visibility

Before the optimization, the RPC provider receives seven separate `eth_call` requests per device over the `ens` Tor isolation key. It sees a Tor exit IP, time, the Universal Resolver and registry targets, encoded device names, requested fields and public return values. A three-record decision issues up to 21 calls.

With Multicall3, the provider receives one `eth_call` per device, or three per three-record decision. It sees the same public information inside one aggregate payload and can directly associate the subcalls in that payload. A warm mesh IP cache omits that field. The fallback issues the individual calls concurrently. Tor still uses the `ens` key for these calls and the separate `pay` key for payments. CCIP-Read stays disabled. No new direct connection, payment change or persistent node state is introduced.

## Verified local measurements

6 November 2026, warm device resolution, local RPC fixture with 50 ms delay per request. The fixture replaces the Tor network transport, retains viem encoding and `ens` isolation, and makes no cloud request. These numbers do not establish the cloud latency targets.

| Flag | Sample 1 | Sample 2 | RPC requests per resolve |
| --- | --- | --- | --- |
| Off | 360.9 ms | 363.1 ms | 7 |
| On | 51.5 ms | 52.6 ms | 1 |

Reproduce the fixture measurement:

```sh
BRAMBLE_RESOLVE_BENCH=1 pnpm --filter @bramble/sidecar exec vitest run test/fast-resolve.test.ts --reporter=verbose --silent=false --disableConsoleIntercept
```

The existing access-flow test passes in both flag modes. Its flag-on in-memory ENS and loopback userspace WireGuard run measures 3.33 ms from revoke POST to MCP denial, without Tor. This measures the local fixture only.

## Cloud acceptance: Open

Infra-Sol-UI owns the nodes under ETH-244. Required evidence is two isolated passes per flag mode at the same revision. Record the revision, UTC times, flag, warmup, Tor readiness, actual Multicall3 success, and request counts from the logging proxy. Verify nonempty Multicall3 bytecode through the existing ENS Tor client with `publicClient.getCode({ address: '0xcA11bde05977b3631167028862bE2a173976CA11' })` before the runs. Do not dump environment values or RPC configuration.

| Beat | Off, pass 1 | Off, pass 2 | On, pass 1 | On, pass 2 |
| --- | --- | --- | --- | --- |
| 1: first denied call | Open | Open | Open | Open |
| 2: approval confirmation | Open | Open | Open | Open |
| 3: first allowed call | Open | Open | Open | Open |
| 5: revoke confirmation | Open | Open | Open | Open |
| 5: revoke started to first denied call | Open | Open | Open | Open |
| Warm gateway decision | Open | Open | Open | Open |

Targets: warm decision below 8 seconds; revoke started to first denied call below 12 seconds. Record revoke-confirmed to denied separately to distinguish chain confirmation from resolver time. External Claude approval and cloud measurements remain required before completion.
