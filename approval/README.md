# Approval API reference

The approval service supports demo beats 2 (request and approve) and 5 (revoke).
It runs in one long-lived Node.js process on the admin machine. Run
`pnpm --filter @bramble/approval dev` from the repository root. The default bind
address is loopback, port 3001. The production commands are `build` and `start`
in the same package. Expose it only through the approved tunnel topology.

This package contains the API only. ETH-30 supplies the real signer and ENS
resolver; ETH-31 supplies the UI. The default runtime accepts requests and deny
decisions, and rejects grant/revoke operations with `503 signer_not_configured`.
Test-only in-memory adapters never enter the runtime path.

All POST requests require `Authorization: Bearer` with the shared token configured
in `APPROVAL_DEMO_TOKEN`. The API never returns the token. Reads are public and use
`Cache-Control: no-store`. The token protects this write surface; it does not
prove an agent's identity. A request is a suggestion, and only a human approver's
explicit action authorizes a grant.

| Method | Path                             | Result                                    |
| ------ | -------------------------------- | ----------------------------------------- |
| POST   | `/api/requests`                  | Create a pending request (201)            |
| GET    | `/api/requests`                  | `{ requests: [...] }`                     |
| GET    | `/api/requests/:id`              | One request                               |
| POST   | `/api/requests/:id/approve`      | Confirm a grant through the signer        |
| POST   | `/api/requests/:id/deny`         | Record denial without signing             |
| POST   | `/api/grants/:agentLabel/revoke` | Clear that agent's ACL through the signer |

The request body is `{ agentLabel, gatewayLabel, service, note?, requestedDuration? }`.
Duration is integer seconds after grant confirmation. See ADRs 0011, 0012 and 0016 for the
contract, limits, error handling and timer limitations. Errors have the shape
`{ error: { code, message } }`. A failed or duplicate action never reports success.

`pnpm --filter @bramble/approval test` exercises the API using an in-memory
signer/resolver. These tests perform no network writes.
