# Public console activity source diagnosis

[ETH-254](/ETH/issues/ETH-254), 6 November 2026. Demo beats 1, 3 and 5.
Source baseline: `hackathon` at `5724e2f`. This is a diagnosis and deployment
handoff, not a claim that the public defect is fixed.

## Verified

At `2026-11-06T13:27:31.336Z`, the unauthenticated public
`GET https://console.13-233-181-104.sslip.io/api/activity` returned HTTP 200:

```json
{"sources":{"approval":"ok","gateway":"not_configured","hedera":"not_configured"}}
```

The response contained 29 events. Its event kinds were `access-granted`,
`access-revoked`, `request-denied` and `request-received`; no gateway, settlement
or measured-cutoff kind was present. `GET /api/payments` returned HTTP 503 with
error code `payments_configuration`. The read-only assertion in
[the configuration procedure](../../deploy/ACTIVITY-SOURCES.md) reproduces the
missing-source failure without accessing any credentials or making decisions.

A second read at `2026-11-06T13:33:33.277Z` returned the same three source
states and four event kinds. `/api/payments` still returned HTTP 503 with
`payments_configuration`. This independently reproduces the failure after the
interrupted investigation resumed. The documented source check ran again at
`2026-11-06T13:35:54.418Z` and exited 1 with
`AssertionError: activity sources are not ready`.

`bun test approval/test/activity.test.ts` passed all 20 existing tests with
77 assertions. These include missing, failed and successful source reads and
cutoff correlation. They validate the source contract locally, not the deployed
revision or a live repair.

[ETH-249](/ETH/issues/ETH-249) supplies the earlier desktop/mobile evidence and
[PR #45](https://github.com/WhyAsh5114/bramble/pull/45). This report reuses that
presentation evidence rather than repeating browser QA.

## Read from source

The source contract identifies absent or empty reader configuration as the
immediate cause of these states. This is an inference from the public response
and the source baseline; the exact deployed SHA remains unverified:

| Reader | Wiring | Meaning of the observed state |
| --- | --- | --- |
| Gateway | `approval/src/app/api/[...path]/route.ts` passes `BRAMBLED_STATUS_URL` to `createStatusSource` | `approval/src/lib/activity.ts` returns `not_configured` before fetching only when the base URL is falsey. A bad URL, timeout, unreachable listener or malformed body returns `unavailable`. |
| Payments | The same route passes `!!APPROVAL_PAYMENT_AGENTS` to `createPaymentsSource` | A false configured predicate returns `not_configured` before calling the payments handler. A truthy but invalid mapping or failed mirror read returns `unavailable`. The separate payment route's 503 corroborates the configuration failure. |
| Cutoff | `buildActivity` joins retained approval revoke timestamps to matching gateway events | With no gateway events, it cannot emit a measured cutoff. Showing an on-chain revoke alone does not measure enforcement. |

This distinguishes missing configuration from an upstream outage or a theme/
layout bug. The public response does not distinguish an unset variable from an
empty one. No environment file or credential was inspected.

At the source baseline, the deployment reference omits both source variables from
`deploy/env/admin.env.example`. `deploy/APPROVAL-USER-SERVICE.md` directs operators
to that template. `deploy/scripts/check-env.sh admin` does not require either
reader; a passing environment check therefore does not establish demo feed
readiness. The existing user console tunnel forwards relay loopback `3001` to
admin loopback `3011`, with no gateway status forward. These are configuration
and verification gaps in the documented deployment path; the operator's actual
provisioning history and any separate private forward remain unverified.

## Disposition and open verification

The PR adds commented source variable names to the admin template and a linked
configuration procedure with a read-only failing/passing check. It preserves the
feed's explicit health states and changes no runtime behavior. This fulfills the
request to file the exact cause in a PR targeting `hackathon`.

Deployment remains separate: establish the private gateway read path, supply the
public payer mapping/window, and coordinate any restart and HCS observer effect.
No cloud node, tunnel, deployed environment or service was changed. No grant,
revocation, payment or HCS transaction was created for this investigation.

The exact deployed SHA, private path reachability, payer attribution, retained
gateway history, existing settlements and a real measured cutoff remain open.
After coordinated configuration, run the source check, inspect existing event
kinds and settlement links, then repeat the public presentation QA. A source
reading `ok` with no events is not complete evidence for beats 1, 3 or 5.
