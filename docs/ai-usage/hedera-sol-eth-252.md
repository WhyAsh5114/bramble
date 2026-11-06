# Hedera-Sol AI usage — ETH-252

Demo beats: 1–5. Swept committed `origin/hackathon` at `4928612` with local
value-redacted pattern and entropy checks. Inspected deployment units, the
installer, approval authentication, signer diagnostics, and MCP provisioning.
No external scanning service or live credential file was used.

Verified: signer diagnostics can expose a synthetic credential; the regression
fails before the logging fix and passes after it. A synthetic key generator
reproduces mode 644 output before the permission fix and mode 600 after it.
The key CLI check also covers public-only output and overwrite refusal.

Read from source: one shared approval token authorizes all POST routes. Removed
the general agent recipe that exports and persists it. The assignment's Pi
token-file exception remains explicit; live file isolation is Open.

Paths written:

- `package.json`
- `.gitignore`
- `agent-mcp/README.md`
- `agent-mcp/REFERENCE.md`
- `approval/src/lib/service.ts`
- `approval/test/api.test.ts`
- `deploy/scripts/gen-node-key.sh`
- `deploy/scripts/gen-node-key.test.sh`
- `docs/ai-usage/hedera-sol-eth-252.md`

Final baseline: merged `origin/hackathon` at `5724e2f` without conflicts and
scanned its 35-file delta (679 tracked files total). The additional URL candidate
is a synthetic credential-rejection fixture. Read from source: join submissions
are public and rate-limited; join decisions and membership lists require the
operator token, and join failures return fixed diagnostics.

Verified: `pnpm verify` passes after the merge, including `pnpm test:deploy`,
formatting, both invariant checks, typechecking, lint, builds, and tests. Turbo
reports 12 successful test tasks, with 11 cached. The changed approval package
runs 118 tests with 622 assertions. The standalone key CLI regression and the
env ignore checks pass. Two-axis local review reports zero findings on each axis.

Open: the operator-only admin CLI entrypoints still print raw failure objects,
which can include a credentialed RPC URL. This is reported as P2 diagnostic
hardening; no operator command with live credentials was run. Deployment artifact
ownership and live Pi isolation also remain reported follow-ups.

Continuation on 6 November 2026: merged `origin/hackathon` at `c8ca61b`
and scanned its four-file documentation delta with local, value-redacted
private-key, provider-token, JWT, credential-assignment, URL, hex, and entropy
checks. No candidates were detected. No live credential file was inspected.
Read from source: the added manual dry-run guide limits token handling to the
operator workstation and keeps the human approval step explicit. Live isolation
remains Open.
