# Rootless access-flow test reference

## Scope

ETH-33 covers demo beats 1, 2, 3 and 5: deny, request, approve, succeed,
revoke, then deny the same service call without restarting either peer.

`pnpm test:access-flow` runs the existing Go gateway scope/revocation test,
then `agent-mcp/test/access-flow.test.ts`. The latter also runs under
`pnpm verify` through the Model Context Protocol (MCP) package's test task.

The test starts:

- The real approval application programming interface (API) and
  `createChainAdapters`, using an in-memory Ethereum Name Service (ENS)
  text-record store and a signer that confirms writes in memory.
- The real MCP standard input/output (stdio) server and software development
  kit (SDK) client. The agent calls both public tools.
- Two userspace WireGuard peers, the gateway CONNECT server, the local
  forwarder, and a loopback Hypertext Transfer Protocol (HTTP) backend in a
  test-only Go subprocess.

The approval signer computes the access control list (ACL) digest through the admincli library.
The Go gateway independently verifies that digest against the requesting
peer's public key, the gateway key, service and trusted granter. The fixture
shares only public keys with the TypeScript driver. Test identities and the
demo token are generated per run; no deployment credentials are required.

## Assertions

| Step | MCP result | Gateway decision | Backend requests |
| --- | --- | --- | --- |
| Before requesting | Denied | No matching granted digest | None |
| Request pending | Denied | No matching granted digest | None |
| After approval | Granted, then task result | Allowed | `/task` once |
| After revocation | Denied | No matching granted digest | Still `/task` once |

Gateway decisions are observed through `Server.OnDecision`; the fixture's
control endpoint cannot grant access. This prevents a broken tunnel from
passing as an ACL denial merely because the forward closes the connection.
The positive control requires the actual backend response over WireGuard.

## Verified measurements

On 6 November 2026, `pnpm test:access-flow` passed on Linux amd64 with
Node.js 24.15.0, pnpm 10.33.3, Bun 1.4.0 and Go 1.27.3.

**Verified: 3.36 ms** from immediately before the revoke POST until the next
identical MCP service call returned denied. The following observation confirmed
the gateway rejected the call for a missing ACL digest and the backend request
count stayed unchanged. This is one sample, not a percentile or live-network
latency claim. Each test run prints its own measurement and fails if it exceeds
5,000 ms.

Conditions: loopback transport, in-memory confirmed ENS writes and fresh reads
for each CONNECT, userspace WireGuard, no Tor, no blockchain finality or ENS
poll interval. Existing established streams are outside this measurement.
Live Sepolia/Hedera, Tor revocation latency and cloud topology remain **Open**.

**Verified:** `pnpm verify` passes on the integrated branch. The MCP package
runs 32 tests, including this flow; the approval package runs 44 tests.
`CI=true TURBO_FORCE=true pnpm verify` also passes with zero Turbo cache hits
in every stage. That run records a separate revocation sample of **2.91 ms**.
The branch stacks ETH-30 (`33f6abd`), ETH-32 (`25a6cb8`), and the ETH-86
verification fixes (`dc03b86`). ETH-86's cross-family review remains pending;
its changes are separate from the ETH-33 test implementation.

**Verified sensitivity checks:** temporary mutations were restored after each
run. Returning a successful revoke without clearing the ACL fails because the
next MCP call still returns the task. Computing a grant digest for another
service fails because the expected successful call remains denied.

## Task source and decisions

Task: extend `pnpm test:access-flow` or add a sibling test to drive the full
loop with the in-memory approval service, MCP tools and userspace WireGuard
gateway; record the measured revocation time as Verified.

Decision: extend the existing root command and keep the fixture in a Go test
file. Exercise public MCP/HTTP interfaces and the real gateway; replace only
the external chain boundary. Use ETH-86's ephemeral-port helper and declare
the test's workspace dependencies for task-cache invalidation. Production
approval, gateway and MCP behavior are unchanged by ETH-33.
