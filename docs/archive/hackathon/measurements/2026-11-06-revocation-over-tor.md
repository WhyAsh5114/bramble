# Revocation latency with the ENS poll over Tor (6 November 2026)

Reference page for the W9 measurement (ETH-61, beats 5 and 6).

## Result

With a five-second poll interval, the time from a revocation to the admission loop's next pass finishing was:

| Run                | Passes (ok / total) | Pass duration, median | Revocation latency, median | p95    | Worst   |
| ------------------ | ------------------- | --------------------- | -------------------------- | ------ | ------- |
| Direct 1, no Tor   | 120 / 120           | 1.91 s                | 3.9 to 4.4 s               | 6.6 s  | 7.3 s   |
| Direct 2, no Tor   | 120 / 120           | 1.90 s                | 3.9 to 4.4 s               | 6.6 s  | 7.1 s   |
| Tor 1              | 93 / 94             | 4.37 s                | 7.0 to 7.6 s               | 21.8 s | 29.0 s  |
| Tor 2              | 78 / 78             | 7.75 s                | 10.4 to 11.5 s             | 15.2 s | 19.3 s  |

Each latency cell is the range across the three read-time bounds described under "Method", for the first read of a pass (the `pubkey` record). The p95 and worst cells use the "start" bound, which gives the largest values.

Findings:

- **Verified:** over Tor the median revocation latency was 7.0 to 7.6 s in run 1 and 10.4 to 11.5 s in run 2. The earlier estimate of 5 to 10 s held for run 1 and was too low for run 2.
- **Verified:** the same code and machine without Tor gave a median of 3.9 to 4.4 s in both runs. The 3.7 s figure in the README is a separate live measurement from 6 September 2026 (chain confirmation to connection drop) and is not directly comparable, because this method excludes chain confirmation.
- **Verified:** Tor raised the median request from 0.27 s to 0.62 s (run 1) and 1.01 s (run 2). One pass reads seven records one after another, so this dominates the pass duration.
- **Verified:** the tail is long. In run 1 one request took 19.1 s and one pass failed ("HTTP request failed") at 530 s. A revocation that lands just before such a pass is seen only when the pass ends. The worst case over all Tor passes was 29.0 s. We did not investigate the cause of the slow request (Open).
- **Verified:** in run 2 the median pass (7.75 s) was longer than the 5 s interval, so the loop ran back to back. Each run started its own Tor client, so the circuits differed. We did not isolate why run 2 was slower (Open).

N is two Tor runs and two direct runs of 600 s each, on one machine and one network. Two runs do not give a distribution of circuits. Treat 7 to 11 s as the observed median range and 29 s as the worst seen, not as a bound.

## Method

The script `sidecar/scripts/revocation-poll.ts` replays a trace. It does not send a revoking transaction.

1. It runs the same loop `brambled` runs, with one peer: one `resolveDevice()` pass, a ticker of period `--ttl` that buffers one tick (Go `time.Ticker`), and the next pass starting when the previous one returns.
2. It timestamps every RPC request inside each pass. A pass sends 7 requests.
3. It replays the trace. For each synthetic revocation instant `t0` (every 50 ms), the first pass whose read of the changed record starts after `t0` sees the revocation. The latency is that pass's end minus `t0`.
4. Because a read happens between its request's start and end, it reports three bounds: the read at the request's start, middle and end. Revocation by clearing `pubkey` is the first request of a pass. The `revoked` record is the second request, and gives values 0.3 to 1.2 s lower.

With N peers, `brambled` resolves them one after another, so a revocation can wait behind up to N-1 other Tor resolves (Read from source, `brambled/admission/loop.go`). This measurement does not cover that.

What the method leaves out: chain confirmation of the revoking transaction, RPC node lag, and the time `brambled` takes to remove the peer from the WireGuard table. The first two add to the real figure. The last is small and was not measured here.

The script reuses the sidecar's own ENS code through the `createEnsClient` seam in `sidecar/src/ens/client.ts`. The Tor runs use `torFetch('ens')`; the direct runs use `fetch`.

## Conditions

- Date and time: runs started at 03:46, 03:56, 04:06 and 04:16 UTC on 6 November 2026, in the order Tor 1, Direct 1, Tor 2, Direct 2. Each ran 600 s.
- Machine: one developer machine on one residential or office network (not a datacenter). This is the **rootless equivalent** of the cloud topology. We did not run it from the cloud machines, so a datacenter's route to the Tor network is Open.
- Software: Bun 1.4.0, tor-js 0.4.4 through `sidecar/src/tor/index.ts`, a fresh Tor client per Tor run. Tor became ready after 5.0 s (run 1) and 4.3 s (run 2).
- Poll interval: `--ttl 5` (5 s).
- Name: `device1` (the `label` field in all four saved run files; the script's default label is `laptop-demo`) under `bramble-dev-c91e55e9.eth` on the hackathon Sepolia ENSv2 deployment.
- RPC: the default public Sepolia endpoint (`ethereum-sepolia-rpc.publicnode.com`), no dedicated provider.

## Reproduce

From `sidecar/`:

```sh
BRAMBLE_TAILNET_NAME=<name>.eth BRAMBLE_TAILNET_REGISTRY=0x<registry> \
  bun run scripts/revocation-poll.ts --mode tor --label device1 --ttl 5 --duration 600 --out tor.json
BRAMBLE_TAILNET_NAME=<name>.eth BRAMBLE_TAILNET_REGISTRY=0x<registry> \
  bun run scripts/revocation-poll.ts --mode direct --label device1 --ttl 5 --duration 600 --out direct.json
```

Unset `SEPOLIA_RPC_URL` to use the public endpoint. The script needs no signing key.

## Not done

- Snapshot bootstrap (a tor-js directory snapshot fetched at startup) is not built. It was cut first, as the handoff allows. Tor became ready in 4.3 to 5.0 s in these runs, so the cold start it would shorten is already short on this network. The 180 s bootstrap case in [ADR 0013](../adr/0013-tor-transport.md) was not measured here.
- No live revocation over Tor with two machines. The measured number combines a real Tor trace with a replay.
