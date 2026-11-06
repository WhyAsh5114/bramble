# How to attach the Bramble agent MCP server to Claude Code

This how-to gives a Claude Code session `call_private_service` through the local
Bramble forward. The session holds no backend credential. A human submits and
approves access requests through trusted operator tooling. Tool and setting details
are in [REFERENCE.md](./REFERENCE.md).

Demo beats served: 1 (denied), 2 (request and approve), 3 (works after approval),
and 5 (denied after revoke).

## Before you start

- Bun is installed, and you ran `pnpm install` at the repository root.
- `brambled` runs on the agent's worker node with a forward to the private service:
  `brambled serve -label <agent-label> -peer <gateway-label> -forward 8080=<gateway-label>:<service>`.
- You know the ENS labels of the worker and gateway.
- The agent's shell and readable configuration contain no `APPROVAL_DEMO_TOKEN`.
  The shared token authorizes approve and revoke as well as request creation.
  Giving it to an agent lets that agent approve its own request.

## Steps

1. Register the server from an agent shell that has no approval token:

   ```sh
   claude mcp add bramble-agent \
     --env BRAMBLE_AGENT_LABEL=<agent-label> \
     --env BRAMBLE_GATEWAY_LABEL=<gateway-label> \
     -- bun run "$PWD/agent-mcp/src/index.ts"
   ```

   Add `--env BRAMBLE_FORWARD_URL=...` if the forward uses another loopback port.
   Set `BRAMBLE_FORWARD_TIMEOUT_MS=120000` when the gateway reads ENS over Tor.
   Do not pass the approval token through `claude mcp add --env`: Claude Code
   persists those settings in a configuration file the agent can read.

2. Start Claude Code and run `/mcp`. The server registers two tools.
   `call_private_service` works without the approval token.
   `request_private_access` returns a missing-token error in this configuration.

3. Give the agent this prompt: "Fetch `/task` from the private staging service.
   If access is denied, wait for the operator to approve access, then retry."

4. Submit a request and approve it using the human-controlled console or trusted
   operator tooling. Keep the approval token outside the agent session.

5. Revoke the grant in the console. The next `call_private_service` must fail.

## Pi demo exception

The ETH-252 assignment explicitly allows the Pi agent's designated token file.
Only that deployment may provision the shared token from its operator-managed
file. This guide does not create, read, or migrate that file. Its contents must
never appear in repository files, MCP registration commands, logs, or task comments.
The Pi exception does not extend to other agents or worker sessions.

The current server reads `APPROVAL_DEMO_TOKEN` from its process environment.
The designated Pi launcher must handle file-to-process provisioning. File location,
permissions, and launcher isolation are Open in this repository sweep: no live
credential file was inspected. An agent with this token can approve and revoke;
the MCP tool list does not restrict the token's authority.

## Check the server without Claude Code

Run `pnpm --filter @bramble/agent-mcp test`. The tests use synthetic tokens and
stub servers on loopback. They do not establish isolation of a live agent session.
