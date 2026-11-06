# ADR 0016 — Grant duration through server-side revoke timers

**Kind:** Explanation  
**Status:** Draft, 6 November 2026  
**Human approver:** Project owner — pending; record the person's name and decision here.  
**Evidence:** Read from source: ETH-29; build brief W1; ADR 0005.  
**Demo beats:** 2 (duration), 5 (revoke).

## Context

The ACL records in ADR 0005 contain capability digests without expiry. A duration
shown in the console must describe a real revoke mechanism rather than imply a
chain feature that does not exist.

## Draft decision

Interpret `requestedDuration` as integer seconds after the grant confirms. A
pending request never starts a timer. On confirmed approval, record `expiresAt`
and schedule a server-side timer to invoke the same signer revoke path used by
the human's Revoke action. No client polling is needed to trigger the timer.

The API exposes `durationMode: server-timer`, UTC `expiresAt`, UTC `serverTime`,
and nonnegative `remainingSeconds`. The later UI can update its countdown from
these timestamps and refresh from GET. An omitted duration yields null countdown
and mode. The API-only ETH-29 does not implement the countdown UI; ETH-31 owns it.

`expiryStatus` reports `none`, `scheduled`, `revoking`, `failed` or `revoked`.
Reaching zero means the server should attempt revoke; it does not mean chain
access has ended. If the write fails, keep the request approved, show `failed`,
and retry every five seconds while this process runs. Never report revoked
until the signer confirms, or a fresh ACL read proves no grant remains. A manual
revoke cancels the timer. A stale callback cannot revoke a later grant created through this API for the
same agent. A fresh empty ACL read at approval also retires older local grant
records and cancels their timers (`revokeReason: external`).

The per-agent write lock also serializes expiry against a manual action. A timer
that meets an in-progress write retries; the API never sends a duplicate
concurrent write for that agent.

## Consequences

This is an operator convenience, not on-chain expiry. Timer execution, chain
confirmation and peer resolution can delay the actual cut-off. The UI must show
the distinction between countdown completion, revoke pending, failure and
confirmed revoke.

Stopping, restarting or suspending the approval process can prevent automatic
revoke. Requests and timers are not restored after restart. The operator must
reconcile outstanding grants through admin tooling before restarting a demo.
Do not claim bounded grant lifetime when the approval service is unavailable.
External revoke/regrant operations that bypass the API are not versioned by the
boolean ACL-presence interface. An outstanding timer can revoke such a replacement.
Route grant changes through this API while timers are active; stop timers only
after reconciling and revoking their grants before using independent admin tools.
The gateway continues to enforce the current ENS ACL independently throughout.

## Alternatives

Omitting duration is valid for a later UI that does not offer it, but ETH-29
explicitly requests timer behavior. Adding expiry to the digest or gateway
verification would change the ACL contract and require a separate human-approved
design. Browser timers would depend on an open page and cannot own revocation.
