# Resolver cloud measurement, 6 November 2026

Reference for demo beats 1, 2, 3, 5 and 6. The measurement slot follows the explicit Pi ownership handoff on [ETH-271](/ETH/issues/ETH-271).

**Blocked:** the lifecycle runner requires `APPROVAL_DEMO_TOKEN`, which is absent from this session. It exits before creating a request. Cloud latency acceptance and the paired gain remain Open. PR #52 stays unmerged; `BRAMBLE_FAST_RESOLVE` stays default OFF.

## Revisions and verification

Measurement revision `824219c878d4e62e36511cfc88163fc07ffb7291` composes the four PR #52 commits through `316bb56` on deployed relay fix `970461c`. Both nodes build and install that revision. `pnpm verify` passes, with 12 successful tasks. `pnpm test:access-flow` passes with the flag unset and with `BRAMBLE_FAST_RESOLVE=1`. These access-flow fixtures use in-memory ENS and loopback WireGuard without Tor.

The local 50 ms RPC fixture passes seven tests. OFF samples are 357.9 and 357.2 ms, with seven requests each. ON samples are 52.9 and 52.3 ms, with one request each. These numbers do not satisfy the cloud merge gate.

## Dedicated RPC compatibility

Verified through the existing `publicClient` from `sidecar/src/ens/client.ts`, after `startTor()`, with its existing RPC configuration and `ens` isolation key. No direct RPC client is created and no RPC configuration is printed.

| Check | UTC | Result |
| --- | --- | --- |
| Multicall3 `getCode` | 16:04:03.997 | 3,808 bytes at `0xcA11bde05977b3631167028862bE2a173976CA11` |
| Gateway resolution | 16:04:07.059 | Aggregate succeeds; status 2; not revoked |
| Worker resolution | 16:04:09.839 | Aggregate succeeds; status 2; not revoked |
| Granter resolution | 16:04:12.618 | Aggregate succeeds; status 2; not revoked |

The diagnostic records three aggregate attempts, three successful aggregates, zero aggregate errors and zero individual fallback reads. Its wrapper counts calls and decoded subcall success. It is a compatibility check, separate from the node lifecycle passes. It does not establish aggregate success or fallback counts for unperformed lifecycle passes.

## Cloud acceptance

Both nodes restart with the flag unset. Targeted checks confirm the flag is absent in each daemon and its Bun sidecar. Tor becomes ready on both. The worker listener becomes available. The gateway auto-discovers nine devices and its listener is observed ready at 16:09:17.778Z, after its 16:04:06Z restart. This approximately 312-second startup bound is separate from warm decision latency.

The runner then exits because its required authentication variable is missing. It makes no access request, approval, revoke or service call. The earlier worker and granter chain prechecks both report status 2, not revoked and zero ACL entries. No unrelated ACL is modified.

| Measurement | OFF pass 1 | OFF pass 2 | ON pass 1 | ON pass 2 |
| --- | --- | --- | --- | --- |
| First denied call | Open | Open | Open | Open |
| Approval confirmation | Open | Open | Open | Open |
| First allowed call | Open | Open | Open | Open |
| Revoke confirmation | Open | Open | Open | Open |
| Revoke started to denial | Open | Open | Open | Open |
| Revoke confirmed to denial | Open | Open | Open | Open |
| Warm gateway decision | Open | Open | Open | Open |
| Logging-proxy RPC counts | Open | Open | Open | Open |

The warm decision target below 8 seconds and revoke-started bound below 12 seconds remain unmeasured. No ON node lifecycle is run. No real paired cloud gain is established. External approval remains Open on the originating change.

## Restoration and shutdown

Both checkouts and installed executables return to `970461c4956e16808ef9d40495f4b3b6dac4872d`. The temporary `/run/systemd/system/bramble-<role>.service.d/90-eth271.conf` files are removed and systemd reloads. Existing persistent environment files remain unchanged.

| Node | Stop verified UTC | ActiveState | SubState | MainPID |
| --- | --- | --- | --- | --- |
| Gateway | 16:10:07 | inactive | dead | 0 |
| Worker | 16:10:11 | inactive | dead | 0 |

The restored revision is built before each service stops. No further restart runs. Shutdown evidence is posted to [ETH-244](/ETH/issues/ETH-244). Neither gateway nor worker service remains running while this task waits.

For provider visibility, see [Resolver provider visibility](../explanation/resolver-provider-visibility.md). An aggregate exposes the same public names and fields in one associated payload. Tor hides the node source IP for ENS reads; it does not hide the payload from the RPC provider.

## Continuation

Infra-Sol-UI retains ownership of the blocked measurement task. An operator must restore the existing `APPROVAL_DEMO_TOKEN` to the runtime through its approved configuration; do not put the value in a comment or document. Once restored, coordinate a new slot through [ETH-244](/ETH/issues/ETH-244), verify the current deployed revision and preserve any later merged fixes when composing the candidate. Run two OFF and two ON passes at one exact revision, then restore and stop both services again. Keep the flag default OFF and the existing merge and review gates.
