# Approval API reference

**Kind:** Reference
**Describes:** the `approval/` package as merged into `hackathon` (PR #11, ETH-29). Source: `approval/src/lib/{contracts,validation,api,service}.ts`.
**Status:** Describes the code, not an accepted design. ADR 0012 and ADR 0016 are drafts, and a human approver is pending for both.
**Demo beats:** 2 (request and approve) and 5 (revoke).

## Endpoints

| Method | Path                             | Auth  | Result                                                                      |
| ------ | -------------------------------- | ----- | --------------------------------------------------------------------------- |
| `POST` | `/api/requests`                  | token | Create a pending request. Returns `201`.                                    |
| `GET`  | `/api/requests`                  | none  | `{ "requests": [...] }`, all requests in memory.                            |
| `GET`  | `/api/requests/:id`              | none  | One request. `404 request_not_found` if unknown.                            |
| `POST` | `/api/requests/:id/approve`      | token | Grant through the signer. Returns the request with `grantTransaction`.      |
| `POST` | `/api/requests/:id/deny`         | token | Record the denial. No signing.                                              |
| `POST` | `/api/grants/:agentLabel/revoke` | token | Clear that agent's ACL grants through the signer. No request id is needed.  |

Reads send `Cache-Control: no-store`.

`GET /api/network` requires the operator token and returns redacted approval-derived
topology, joins, members and service request states, with optional gateway observations. `POST /api/network/ping`
requires the token and probes a server-known relay/rendezvous HTTP target or an optionally observed device. See the
[console network reference](approval-network.md) for fields and observation limits.

## Request body

`POST /api/requests` takes `application/json`.

| Field               | Type    | Rule                                                                                         |
| ------------------- | ------- | -------------------------------------------------------------------------------------------- |
| `agentLabel`        | string  | Required. Lowercase ENS label, 1–63 characters (`[a-z0-9-]`, no leading or trailing hyphen). |
| `gatewayLabel`      | string  | Required. Same rule.                                                                         |
| `service`           | string  | Required. Symbolic name, `^[a-z][a-z0-9_-]{0,63}$`. Never a host or port.                    |
| `note`              | string  | Optional. At most 2000 characters.                                                           |
| `requestedDuration` | integer | Optional. Seconds after the grant confirms, 1 to 86400. See [Duration](#duration).           |

The service rejects unknown fields, a body over 16384 bytes, and any content type other than `application/json`.

## Request record

Every response that carries a request has these fields.

| Field                 | Type   | Meaning                                                                                  |
| --------------------- | ------ | ---------------------------------------------------------------------------------------- |
| `id`                  | string | Random UUID.                                                                             |
| `status`              | string | See [Status values](#status-values).                                                     |
| `createdAt`           | string | UTC timestamp.                                                                           |
| `decidedAt`           | string | Set on approve or deny.                                                                  |
| `grantTransaction`    | object | `{ transactionHash }`. Set when the signer confirms the grant.                           |
| `revokeTransaction`   | object | `{ transactionHash }`. Set only when the signer confirms a revoke. Absent otherwise.     |
| `revokedAt`           | string | UTC timestamp. Set when the service marks the request `revoked`.                         |
| `revokeReason`        | string | `manual`, `duration` or `external`. See [Status values](#status-values).                 |
| `expiresAt`           | string | UTC timestamp. Set on approval when `requestedDuration` was given.                       |
| `serverTime`          | string | UTC timestamp of the response.                                                           |
| `remainingSeconds`    | number | Nonnegative seconds to `expiresAt`, or null without a duration.                          |
| `durationMode`        | string | `server-timer` when a duration was given, otherwise null.                                |
| `expiryStatus`        | string | `none`, `scheduled`, `revoking`, `failed` or `revoked`.                                  |

## Status values

| Status      | Meaning                                                                                                                                         |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `pending`   | Waiting for a human.                                                                                                                            |
| `approving` | The signer is writing the grant. Returns to `pending` if the write is not confirmed.                                                            |
| `approved`  | The signer confirmed the grant transaction.                                                                                                     |
| `denied`    | A human denied the request. Terminal.                                                                                                           |
| `revoking`  | The signer is writing the revoke. Returns to `approved` if the write is not confirmed.                                                          |
| `revoked`   | The agent has no grant, by one of the paths in the next table. Terminal.                                                                        |

`revoked` does not always mean this service sent a revoke transaction. `revokeReason` and `revokeTransaction` tell the paths apart.

| `revokeReason` | How the request reaches `revoked`                                                                                       | `revokeTransaction` |
| -------------- | ----------------------------------------------------------------------------------------------------------------------- | ------------------- |
| `manual`       | A human calls the revoke endpoint and the signer confirms.                                                              | Set                 |
| `duration`     | The expiry timer calls the same revoke path and the signer confirms.                                                    | Set                 |
| `external`     | A later approve for the same agent reads an empty ACL, so the service retires the older `approved` request.             | Absent              |

A client shows a transaction link only when `revokeTransaction` is present.

A confirmed local grant proceeds to a revoke transaction even when an ENS read
still returns an older empty ACL. Manual and timed revokes require the revoke's
own confirmed receipt. A submitted write with an unknown outcome remains
unresolved and blocks another write for that agent. Without a confirmed local
grant, the service checks ENS and returns `409 not_granted` for an empty ACL.

## Duration

Duration is a server-side timer, not a chain feature. The ACL records in ADR 0005 hold no expiry.

- A pending request never starts a timer.
- On a confirmed approval with `requestedDuration`, the service sets `expiresAt` and schedules a revoke after that many seconds.
- When the timer fires, it runs the revoke path. If the signer fails, the request stays `approved`, `expiryStatus` becomes `failed`, and the service retries every 5 seconds while the process runs.
- A timer that meets an in-progress write for the same agent retries after 5 seconds.
- A manual revoke cancels the timer.
- The timer lives in process memory. A restart drops every request and every timer, and the grant stays on chain.
- Reaching zero means the service attempts a revoke. It does not mean chain access has ended. Check `expiryStatus` and `revokeTransaction`.

A human still approves every grant. A duration does not approve anything.

## Writes and authentication

Except for `POST /api/joins`, every `POST` requires `Authorization: Bearer <token>`, compared in constant time with the `APPROVAL_DEMO_TOKEN` environment variable. If the variable is unset, writes fail with `503 token_not_configured`. A wrong or missing token returns `401 unauthorized`. The service never returns or logs the token. Access-request reads need no token. Join-list and member-list reads require the same token. Individual join status reads remain open.

The token gates the write surface. It does not prove who the agent is, and it does not authorize a grant. The service never auto-approves. Before approving, it re-reads the agent's and the gateway's ENS identity through the resolver and requires an active identity with a published public key.

## Errors

Errors use `{ "error": { "code": "<snake_case>", "message": "<text>" } }`.

| Status   | Code                                             | When                                                                  |
| -------- | ------------------------------------------------ | --------------------------------------------------------------------- |
| 400      | `invalid_request`, `invalid_json`                | Body or field fails validation.                                       |
| 401      | `unauthorized`                                   | Wrong or missing token.                                               |
| 404      | `request_not_found`, `route_not_found`           | Unknown id or path.                                                   |
| 409      | `request_not_pending`                            | Approve or deny on a request that already left `pending`.             |
| 409      | `already_granted`                                | The agent already has a grant, so a second approve is refused.        |
| 409      | `identity_not_active`                            | Agent or gateway has no active ENS identity with a published key.     |
| 409      | `not_granted`                                    | Revoke for an agent with no grant.                                    |
| 409      | `operation_in_progress`                          | Another write for the same agent is running.                          |
| 413, 415 | `request_too_large`, `unsupported_media_type`    | Body size or content type.                                            |
| 502      | `grant_failed`, `revoke_failed`                  | The signer did not confirm.                                           |
| 503      | `signer_not_configured`, `token_not_configured`  | Missing runtime configuration.                                        |

A double approve, or a revoke of an ungranted agent, returns a 409 before any signer call. A failed or duplicate action never reports success.

## Verification status

- **Verified:** on 6 November 2026, at `origin/hackathon` `2403c62`, `pnpm --filter @bramble/approval test` passes 21 tests with 177 assertions (Bun 1.4.0). The tests use the in-memory signer and resolver.
- **Read from source:** every value above comes from `approval/src/lib` on `hackathon`.
- **Open:** the 2 s visibility target against a running service and a real signer.


## Device joins and membership

These endpoints implement ETH-177 shared contract v1 and the Atlas list-row decision on ETH-181. Device membership is separate from service ACL grants. Approving a join enrolls a device; it does not grant a private service.

| Method | Path | Auth | Result |
| --- | --- | --- | --- |
| POST | `/api/joins` | none | `201 {id,status:"pending"}` |
| GET | `/api/joins/:id` | none | One join record; `404 join_not_found` for an unknown ID |
| GET | `/api/joins` | token | `{joins:[...]}` |
| POST | `/api/joins/:id/approve` | token | Confirm enrollment and return the approved record |
| POST | `/api/joins/:id/deny` | token | Deny a pending suggestion without signing |
| POST | `/api/joins/:id/revoke` | token | Confirm `revoked=true` through admincli and return the revoked record |
| GET | `/api/members` | token | `{members:[...]}`, read from ENS registration events and current records |

`POST /api/joins` takes `{label,pubkey,tailnet}`. The label follows the access-request label rule and the full device name must pass ENS normalization. `pubkey` is exactly 64 hexadecimal characters representing the 32-byte WireGuard public key, normalized to lowercase. `tailnet` must match `APPROVAL_ENS_PARENT`. Unknown fields, malformed JSON and bodies over 16384 bytes are rejected.

The open endpoint accepts at most 30 attempts per rolling minute per process, including invalid submissions. It returns `429 rate_limited` and `Retry-After: 60` above the limit. The limit is global and does not trust forwarded client-IP headers. The process holds at most 1000 join records; a full queue returns `503 queue_full`. Pending requests, history, allocation progress and rate-limit counters remain in memory. A restart drops them.

Every join and member row carries `id`, `label`, `tailnet`, and `status`. Status is `pending`, `approved`, `denied`, or `revoked`. Additional fields are `pubkey`, `createdAt`, `decidedAt`, `meshIp`, `meshCidr`, `enrollTransactions`, `revokeTransaction`, `revokedAt`, `inProgress`, and `failure` when applicable. `meshIp` is a plain address such as `10.77.0.2`; `meshCidr` is the on-chain value such as `10.77.0.2/24`.

Approval calls `admincli.enrollDevice`: deploy a dedicated resolver, register the subname, publish the public key, and allocate and publish `mesh-ip` per ADR 0009. The API reports `approved` only after transaction confirmation and read-back. `enrollTransactions` holds the confirmed `deploy`, `register`, `pubkey`, and `meshIp` hashes. Revoke calls `admincli.setDeviceRevoked`; `revoked` requires its own confirmed transaction receipt. A stale or unavailable read-back never turns that confirmed receipt into another submission. A retained revoke receipt also prevents a stale member scan from reverting its status. Peers enforce revocation through their own ENS checks.

`inProgress` is `approving` or `revoking` while an action runs. The stable status remains `pending` or `approved` until confirmation. Concurrent writes return `409 operation_in_progress`. An incomplete enrollment reserves its allocation: retry that join before approving another device (`409 enrollment_incomplete`). Deny is refused after enrollment starts (`409 enrollment_started`); finish approval and then revoke. A failed write returns `502 enroll_failed` or `502 revoke_failed` with fixed text; adapter diagnostics are suppressed. A receipt timeout retains the submitted hash inside the running process; retry checks that receipt instead of submitting it again.

An existing label returns `409 label_taken` before enrollment. A partial registration can resume only when it points to the resolver created for that enrollment. Duplicate or out-of-order decisions return `409 join_not_pending` or `409 join_not_approved`.

Member enumeration includes unexpired registrations, including revoked devices. A partially enrolled device owned by this queue remains `pending` in the member list. After a restart, discovered members use IDs of the form `member:<label>`; pass the URL-encoded ID to the same revoke endpoint. Join IDs and partial enrollment progress do not survive a restart. Reconcile any interrupted enrollment on chain before reusing its label or starting a competing enrollment process. Run a single approval-service process for this operator-driven allocation flow.

## Enrollment configuration

| Variable | Purpose |
| --- | --- |
| `APPROVAL_ENS_PARENT` | ENS tailnet served by joins; must match `BRAMBLE_TAILNET_NAME` |
| `APPROVAL_DEMO_TOKEN` | Existing shared operator token; never sent by joining devices |
| `SEPOLIA_PRIVATE_KEY` | Existing software Sepolia transaction signer |
| `BRAMBLE_TAILNET_NAME`, `BRAMBLE_TAILNET_REGISTRY` | Existing tailnet configuration |
| `BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK` | First block for bounded device registration-log scans |
| `SEPOLIA_RPC_URL` | Administrator RPC endpoint; never included in public node configuration |

The existing ACL granter still uses `BRAMBLE_GRANTER_KEY_RING_PATH` and `BRAMBLE_RING_CLI=deploy/scripts/software-ring.sh`. Enrollment uses the same software Sepolia account and does not require Ledger hardware. Its on-chain transactions use testnet funds.

The existing console polls protected joins every second after unlock. ENS member scans run independently every 10 seconds and refresh after actions. The components `PendingJoins`, `Members`, and `JoinActivity` are exported by `@bramble/join-ui`. They take rows, a disabled flag, an optional busy ID, an action callback and an optional button component. They contain no token storage or HTTP client. The local UI admin view can supply rows from `/api/admin/joins` and `/api/admin/members`, with action callbacks to `/api/admin/joins/:id/:action`.

## Public node configuration input

`node scripts/print-node-config.mjs <version>` prints the JSON served at `/.well-known/bramble.json`. It reads only the public variable allowlist in `deploy/scripts/gen-node-config.mjs`. The install-host generator and this command share `buildConfig`.

Required inputs are `BRAMBLE_TAILNET_NAME`, `BRAMBLE_CHAIN_ID`, `BRAMBLE_TAILNET_REGISTRY`, `BRAMBLE_PUBLIC_RPC_URL` (or `BRAMBLE_RPC_HOST`), and `BRAMBLE_APPROVAL_URL` (or `BRAMBLE_CONSOLE_HOST`). Optional inputs are `BRAMBLE_RELAY_REGISTRY`, `BRAMBLE_RENDEZVOUS`, `BRAMBLE_STUN_SERVERS`, `BRAMBLE_DATA_RELAYS`, `BRAMBLE_RELAY_PEERS`, and `BRAMBLE_PUBLIC_SERVICES`. The version can also come from `BRAMBLE_VERSION`.

The output contains `version`, `tailnet`, `chainId`, `rpcUrl`, `registry`, `joinApi`, and optional relay, rendezvous, STUN and service fields. URL validation rejects user info, query strings, fragments and credential-shaped path segments. The generator never uses `SEPOLIA_RPC_URL` as the published RPC URL. The complete launcher schema belongs to `docs/reference/node-config.md` (ETH-180).

## Join verification

- **Verified:** on 6 November 2026, `pnpm verify` passes. Approval tests: 90 tests and 516 assertions. HTTP lifecycle, token checks, validation, rate limiting, retries, allocation reservation, and restart-member revocation use the in-memory chain and admincli.
- **Verified:** eight Playwright checks pass at 1280 px and 390 px, including a join appearing within two seconds and approve/revoke/deny. Tests use ports 13181/13182 and the in-memory chain; they do not prove public-host latency or a Sepolia transaction.
- **Verified:** the receipt-timeout test uses a loopback JSON-RPC boundary and confirms that a retry sends one transaction total.
- **Open:** the opt-in Sepolia smoke (`SEPOLIA_PRIVATE_KEY="${SEPOLIA_PRIVATE_KEY:-$SEPOLIA_PK}" pnpm --filter @bramble/approval exec bun scripts/smoke-join.ts`) requires the administrator configuration. On 6 November 2026, the local attempt stopped before signing because `APPROVAL_ENS_PARENT` was absent.
