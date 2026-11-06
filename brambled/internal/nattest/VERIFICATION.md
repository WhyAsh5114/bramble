# ETH-38 verification evidence

Initial run on 6 November 2026 IST from branch `qa-sol/eth-38-simulated-nat`, based
on `hackathon` commit `3bec957`. Go commands use
`PATH=/home/anonjr/.local/bin:$PATH` with installed Go 1.27.3. Dependency
installation uses pnpm 10.33.3. This report covers simulated UDP behavior only.

| Evidence | Command                                                                   | Result                                         |
| -------- | ------------------------------------------------------------------------- | ---------------------------------------------- |
| Verified | `pnpm install --frozen-lockfile`                                          | Pass; lockfile unchanged                       |
| Verified | `cd brambled && go test -race -tags nattest ./internal/nattest -count=20` | Pass, 11.022 s reported by Go                  |
| Verified | `cd brambled && go test -tags nattest ./...`                              | Pass, all brambled packages                    |
| Verified | `pnpm test:access-flow`                                                   | Pass                                           |
| Verified | `pnpm check:no-endpoints`                                                 | Pass                                           |
| Verified | `pnpm check:no-local-state`                                               | Pass                                           |
| Verified | `pnpm typecheck`                                                          | Pass; Turbo cache used for unchanged packages  |
| Verified | `pnpm lint`                                                               | Pass; Turbo cache used for unchanged packages  |
| Verified | `pnpm build`                                                              | Pass; Turbo cache used                         |
| Verified | `pnpm verify`                                                             | Fails at 50 existing skill-formatting errors   |
| Verified | `pnpm test`                                                               | Fails in existing live Sepolia label-scan test |

**Verified:** the first full-cone test fails to build before the fixture exists,
then passes after implementation. Restricted-cone and symmetric tests first
fail because unsolicited packets are delivered; both pass after adding their
mapping and filtering rules. Twenty race-enabled runs pass after all fixture
changes. Self-tests cover all three NAT types, separate private sockets,
two-NAT probes, blocked direct traffic, local relay forwarding, restoration,
datagram truncation, empty packets, deadlines, concurrent writes, and closure.

**Read from source:** the repository's two-axis `code-review` skill runs against
`git diff 3bec957...HEAD`. Its Standards and Spec reviewers report zero actionable
findings. The Spec reviewer also runs the focused tagged tests successfully.
These local reviews do not replace the required cross-family review handoff.

**Verified:** no changes in this branch fix or bypass the baseline failures.
The failing workspace test is
`sidecar/test/devices.test.ts > listDeviceLabels > scans real registered labels
on Sepolia, read-only`, reporting RPC error code 4444. The complete brambled
suite passes when run separately after Turbo aborts its run on that failure.

**Read from source:** ETH-75 owns the formatting correction. ETH-86 owns the
live RPC verification blocker and names an archive-capable `SEPOLIA_RPC_URL`
as its environment requirement. No credential value is read or recorded here.

## Verification after prerequisite integration

**Verified (6 November 2026 IST):** merged `origin/hackathon` at `4a8cfc8`
without rebasing. Kept both sides of the `docs/AI_USAGE.md` conflict.
The branch then passes `pnpm verify`, including endpoint and local-state
checks and all tagged brambled tests. Unchanged workspace packages use Turbo's
shared cache. `pnpm test:access-flow` passes separately. A new
`go test -race -tags nattest ./internal/nattest -count=20` run passes in
11.020 seconds. The earlier formatting and pruned-history failures above
are historical evidence; ETH-75 and ETH-86 provide the integrated corrections.

**Verified:** also merged the next `hackathon` update, `c1c05bd`, which adds
the W9a signer audit, before the final pre-push checks.

**Read from source:** Atlas requests Net-Claude or Core-Claude for the
cross-family review. The PR handoff selects Net-Claude, whose Claude family
differs from QA-Sol's OpenAI family.

**Open:** push the verified branch, open the PR against `hackathon`, create
the Net-Claude review issue, and await the review and merge. Production STUN,
WireGuard traversal, automatic fallback, and two-machine real-NAT verification
remain outside this fixture's evidence.
