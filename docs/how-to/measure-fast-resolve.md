# Measure flagged resolver reads

This procedure serves demo beats 1, 2, 3, 5 and 6. Infra-Sol-UI owns cloud access under ETH-244. Coordinate the slot there before touching nodes. Use the existing dedicated RPC configuration and ENS Tor client. Never dump environment values or RPC configuration.

## Local fixture

Run the warm-device benchmark with a simulated 50 ms delay per RPC request:

```sh
BRAMBLE_RESOLVE_BENCH=1 pnpm --filter @bramble/sidecar exec vitest run test/fast-resolve.test.ts --reporter=verbose --silent=false --disableConsoleIntercept
```

Label its results as a local fixture without Tor network latency. Keep cloud claims separate.

## Cloud pair

1. Record the exact PR revision and reserve an isolated slot through ETH-244. Preserve the existing stopped-node handoff for ETH-243. Do not overlap grants, revokes or node restarts with another run.
2. Build and install that revision on both nodes within the owner's slot. Keep the existing persistent environment files unchanged. Start with the flag unset, and wait for Tor readiness and startup resolution to finish.
3. Verify nonempty Sepolia Multicall3 bytecode through the existing ENS Tor client: `publicClient.getCode({ address: '0xcA11bde05977b3631167028862bE2a173976CA11' })`. Record byte length and UTC time. Do not create a direct RPC client.
4. Run beats 1, 2, 3 and 5 twice with the flag off, using the existing approved beat runner. Check chain state before each pass and reset grants within the coordinated sequence.
5. Restart both nodes with temporary process environment `BRAMBLE_FAST_RESOLVE=1` for measurement. Verify the flag reaches the sidecars, await Tor and prewarming, and repeat both passes. Do not persist the flag yet.
6. For every pass, record UTC start, flag, revision, first denied call, approval confirmation, first allowed call, revoke confirmation, revoke-started to first denial, revoke-confirmed to denial, and warm gateway decision. Record actual Multicall3 success and RPC request counts from the logging proxy. Do not log private keys, tokens or authenticated RPC URLs.
7. Post the completed table on ETH-267 and link it on ETH-244. Compare warm decisions against 8 seconds and revoke-started to denial against 12 seconds. Record failures and fallback use. Persist the flag in cloud environment files only after measurements support enabling it and the review gate passes.
8. Stop and hand back the nodes as the owner requires. Leave external Claude approval and unmet targets marked Open.

Use the table in the [resolver reference](../reference/fast-resolve.md#cloud-acceptance-open). Explain what the provider sees using [Resolver provider visibility](../explanation/resolver-provider-visibility.md).
