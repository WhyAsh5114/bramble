# Agent payment feed reference

## Evidence and human verification

- **Verified:** rootless Go tests for rendezvous, data-relay selection/stall, and
  main-package relay/payment paths pass on this branch. They use local fixtures;
  they do not settle a funded transaction.
- **Verified:** `approval/test/payments.test.ts` exercises the HTTP mirror adapter
  against a local fixture. It covers page-overlap deduplication, original plus
  `DUPLICATE_TRANSACTION` and child records in both orders, successful outgoing
  USDC only, both reporting-window bounds, exact atomic totals, budget overrun,
  a newly appearing settlement, distinct payers, duplicate payer/label rejection,
  cap validation/defaults, coalesced polls, cache expiry/config invalidation,
  configuration errors and incomplete-feed failures.
- **Read from source:** `sidecar/src/payments/client.ts`, `datarelay.ts`,
  `brambled/rendezvous/client.go`, `brambled/datarelay/`, and `brambled/main.go`
  establish the trigger sequence in the flow guide. Every metered relay buys a
  fresh token on each concurrent exchange attempt, on both nodes. Failed
  admission exchanges retry each sync; data-relay adoption also re-exchanges
  repeatedly and backs off only after two idle minutes. Service-ACL approvals
  do not initiate payments. No production node files change here.
- **Open — needs human verification:** funded testnet x402 settlement, dedicated
  payer attribution in the deployed topology, mirror indexing latency within a
  few seconds, matching displayed and enforced sidecar cap, and HashScan links
  for a real rehearsal transaction. Run two resets and record both latencies.
- **Open — UI gate H4:** a different Sol tester must capture the public console
  at 1280 px and 390 px; the human approves those screenshots. Fixture tests and
  local screenshots do not satisfy that public UI gate.

The adapter fetches all pages in a fixed reporting window (maximum 100 pages,
8-second request deadline). It fails the entire response on an incomplete feed.
It deduplicates consensus records, not transaction IDs: retries and child
transactions may share an ID, and a failed retry must not erase a success.
Concurrent requests with the same configuration share one upstream fetch. A
single in-memory cache entry retains the result, including errors, for one
second after completion; a configuration change invalidates it immediately.
Responses to browsers remain `no-store`. Errors use `{error: {code, message}}`:
invalid configuration (including a future window) returns 503, and an incomplete
or unavailable mirror feed returns 502. An unset approval cap defaults to
`100000`; it accepts the sidecar's positive-integer format, including leading
zeroes. This displayed value is not read from the deployed sidecar.
A failure retains the previous display with a stale warning; it never reports
zero spend as a substitute. Spend includes every net outgoing transfer of testnet
USDC (`0.0.429274`) by the configured payer, including non-relay transfers if the
account is reused. Mirror data alone cannot prove which process signed a payment.
