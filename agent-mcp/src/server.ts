import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import type { Tools } from './tools'

export function createServer(tools: Tools): McpServer {
  const server = new McpServer({ name: 'bramble-agent', version: '0.1.0' })

  server.registerTool(
    'request_private_access',
    {
      description:
        'Ask a human approver for access to a private service on the Bramble network. Blocks until the human decides. Returns "granted", "denied: ..." or "pending: ...".',
      inputSchema: {
        service: z.string().describe('Name of the private service, for example "staging-api".'),
        reason: z.string().describe('Why you need access. The approver reads this.'),
        durationSeconds: z
          .number()
          .int()
          .positive()
          .optional()
          .describe('Optional: how many seconds the access should last after approval.'),
      },
    },
    async (input) => ({ content: [{ type: 'text', text: await tools.requestPrivateAccess(input) }] })
  )

  server.registerTool(
    'call_private_service',
    {
      description:
        'GET a path on the private service through the local Bramble forward. Returns "task result: ..." on success or "denied: no grant" when access is not granted.',
      inputSchema: { path: z.string().describe('Absolute path on the private service, for example "/task".') },
    },
    async (input) => ({ content: [{ type: 'text', text: await tools.callPrivateService(input) }] })
  )

  return server
}
