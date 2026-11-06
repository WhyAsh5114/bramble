# Agent MCP server reference

The server speaks MCP over stdio. Start it with `bun run agent-mcp/src/index.ts`.

## Tools

| Tool                     | Input                                                                          | What it does                                                                                                                                                  |
| ------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `request_private_access` | `service` (string), `reason` (string), `durationSeconds` (optional integer >0) | Posts `POST /api/requests` to the approval API, then polls `GET /api/requests/:id` until the human decides or `BRAMBLE_DECISION_TIMEOUT_MS` passes.           |
| `call_private_service`   | `path` (string, starts with a single `/`)                                      | Sends `GET <path>` to the Bramble forward (`BRAMBLE_FORWARD_URL`). Refuses `//`, backslashes, `..` segments and any path that would leave the forward's host. |

A second `request_private_access` for the same service while a request is still
undecided resumes polling that request. It does not create a duplicate card.

## Outcomes

| Text                                          | Meaning                                                                                                                                               |
| --------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `granted: ...`                                | Request status `approved`.                                                                                                                            |
| `denied: a human rejected the request ...`    | Request status `denied`.                                                                                                                              |
| `denied: access to <service> was revoked.`    | Request status `revoking` or `revoked`.                                                                                                               |
| `pending: no decision after Ns. ...`          | The wait ended with no decision. The request stays open.                                                                                              |
| `task result: <body>`                         | The forward returned a 2xx response. The body is cut at 4000 characters.                                                                              |
| `denied: no grant. ...`                       | The forward answered 403, or closed the connection with no reply. `brambled` closes the connection on a gateway DENY. A broken tunnel looks the same. |
| `error: the Bramble forward is not listening` | Nothing accepts connections at `BRAMBLE_FORWARD_URL`.                                                                                                 |
| `error: ...`                                  | Bad input, a missing token, an unreachable approval API, or an approval API refusal. The text includes the API error code and its message.            |

## Environment variables

| Variable                      | Required | Default                 | Meaning                                                                          |
| ----------------------------- | -------- | ----------------------- | -------------------------------------------------------------------------------- |
| `BRAMBLE_AGENT_LABEL`         | yes      |                         | ENS label the worker node is enrolled under. Sent as `agentLabel`.               |
| `BRAMBLE_GATEWAY_LABEL`       | yes      |                         | Gateway's ENS label. Sent as `gatewayLabel`.                                     |
| `APPROVAL_DEMO_TOKEN`         | for POST |                         | Shared write token; designated Pi exception only. Omit for other agent sessions. |
| `BRAMBLE_APPROVAL_URL`        | no       | `http://127.0.0.1:3001` | Approval API base URL.                                                           |
| `BRAMBLE_FORWARD_URL`         | no       | `http://127.0.0.1:8080` | Bramble forward. Must be a loopback address.                                     |
| `BRAMBLE_POLL_INTERVAL_MS`    | no       | `1000`                  | Delay between decision polls.                                                    |
| `BRAMBLE_DECISION_TIMEOUT_MS` | no       | `120000`                | How long `request_private_access` waits before returning `pending`.              |
| `BRAMBLE_FORWARD_TIMEOUT_MS`  | no       | `10000`                 | Timeout for one `call_private_service` request.                                  |

The server exits with a message on stderr when a required variable is missing or a value is invalid.

## Approval authority

`APPROVAL_DEMO_TOKEN` permits all approval API writes, including approve and revoke.
The tool list does not constrain a process that can read it. General agent sessions
must omit the variable and use human-controlled request submission. Only the
designated Pi token-file deployment is excepted (see [the setup guide](./README.md#pi-demo-exception)).
