# How to attach the Bramble agent MCP server to Claude Code

This how-to gives a Claude Code session two tools: `request_private_access` and
`call_private_service`. The session can then ask a human for access to a private
service and call it through the local Bramble forward. The session holds no backend
credential: the private service sits behind the forward. It does hold the shared demo
token for the approval API, which the agent can read until the API splits tokens (see
[Know the limits](#know-the-limits)). Tool and setting details are in
[REFERENCE.md](./REFERENCE.md).

Demo beats served: 1 (denied), 2 (request), 3 (works after approval) and 5 (denied after revoke).

## Before you start

- Bun is installed, and you ran `pnpm install` at the repository root.
- The approval service runs and you know its URL (default `http://127.0.0.1:3001`).
- `brambled` runs on the agent's worker node with a forward to the private service:
  `brambled serve -label <agent-label> -peer <gateway-label> -forward 8080=<gateway-label>:<service>`.
- You know the ENS label the worker node is enrolled under (`<agent-label>`) and the
  gateway's label (`<gateway-label>`). The console shows the agent under this label.
- You have the shared demo token. Never write it into a file in the repository.

## Steps

1. Export the settings in the shell that starts Claude Code:

   ```sh
   export BRAMBLE_AGENT_LABEL=<agent-label>
   export BRAMBLE_GATEWAY_LABEL=<gateway-label>
   export APPROVAL_DEMO_TOKEN=<shared demo token>
   ```

2. Register the server. Run this once from the repository root:

   ```sh
   claude mcp add bramble-agent \
     --env BRAMBLE_AGENT_LABEL="$BRAMBLE_AGENT_LABEL" \
     --env BRAMBLE_GATEWAY_LABEL="$BRAMBLE_GATEWAY_LABEL" \
     --env APPROVAL_DEMO_TOKEN="$APPROVAL_DEMO_TOKEN" \
     -- bun run "$PWD/agent-mcp/src/index.ts"
   ```

   Add `--env BRAMBLE_APPROVAL_URL=...` or `--env BRAMBLE_FORWARD_URL=...` when the
   defaults do not match your setup.

   Alternatively, put this in a project `.mcp.json`. Claude Code expands the
   `${...}` values from your shell, so the file holds no secret:

   ```json
   {
     "mcpServers": {
       "bramble-agent": {
         "command": "bun",
         "args": ["run", "agent-mcp/src/index.ts"],
         "env": {
           "BRAMBLE_AGENT_LABEL": "${BRAMBLE_AGENT_LABEL}",
           "BRAMBLE_GATEWAY_LABEL": "${BRAMBLE_GATEWAY_LABEL}",
           "APPROVAL_DEMO_TOKEN": "${APPROVAL_DEMO_TOKEN}"
         }
       }
     }
   }
   ```

3. Start Claude Code and run `/mcp`. The server `bramble-agent` shows as connected
   with two tools.

4. Give the agent this prompt: "Fetch the task from the private staging service at
   `/task`. If you are denied, request access and try again."

## What you see

1. `call_private_service` returns `denied: no grant`.
2. `request_private_access` returns nothing while a human decides. The request card
   appears in the approval console.
3. After approval it returns `granted`. The gateway applies the grant on its next
   ENS read, so the first call can still be denied for a few seconds. The agent retries.
4. `call_private_service` returns `task result: ...`.
5. After a human clicks Revoke, `call_private_service` returns `denied: no grant` again.

## Check the server without Claude Code

Run `pnpm --filter @bramble/agent-mcp test`. The tests start a stub approval API and a
stub forward on loopback and drive the server over the MCP protocol.

## Know the limits

- Verified: the tools against stub servers.
- Verified by hand, with no test in this repository: the tools against the approval API
  handlers from ETH-29 (commit `8d5cb3b`) with in-memory chain adapters. Request,
  approve and deny returned `granted` and `denied`.
- Open: a real Claude Code session, a real `brambled` forward, and the live approval console.
- Open: whether Claude Code's Bash tool inherits the exported `APPROVAL_DEMO_TOKEN`.
- Reported by the ETH-102 review, reproduced there: `claude mcp add --env` writes the token in plaintext to Claude
  Code's config file, which the agent can read.
- Open: the shared demo token also authorizes `approve` and `revoke` in the current
  approval API. This server only calls the create-request endpoint, but any process that
  can read the token can approve its own request. Keep the token out of the agent's
  reach for the real demo, or add a request-only token to the approval API.
