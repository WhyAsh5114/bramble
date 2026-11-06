# Infra-Sol-UI: ETH-271

Demo beats: 1, 2, 3, 5 and 6.

Composed the four existing PR #52 commits on relay fix `970461c` in an owned worktree. The resulting measurement revision is `824219c`. No new runtime behavior is written. The source composition affects `brambled/gateway/acl.go`, `brambled/gateway/acl_parallel_test.go`, `brambled/main.go`, `brambled/sidecar/fast.go`, `sidecar/src/ens/client.ts`, `sidecar/src/ens/config.ts`, `sidecar/src/ens/fast-resolve.ts`, `sidecar/src/ens/relays.ts`, `sidecar/src/ens/slow-text.ts`, `sidecar/test/fast-resolve.test.ts`, and the original resolver documentation and source-author usage file.

Wrote `docs/reference/resolver-cloud-measurement.md` and this file. Measurement helpers and evidence live under `/home/anonjr/bramble-work/status/eth-271*`: the diagnostic, lifecycle runner, flag check and readiness helper. They contain no credentials. The Paperclip report links the uploaded evidence and records continuation.

Verified: `pnpm verify`, access-flow checks in both flag modes, the local benchmark, ENS-over-Tor bytecode and aggregate compatibility, restoration to `970461c`, and final stopped services. Blocked: the runtime lacks `APPROVAL_DEMO_TOKEN`. No lifecycle pass runs; no cloud speedup or latency target is claimed.
