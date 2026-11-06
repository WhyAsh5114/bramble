import { afterAll, beforeAll, expect, test } from 'bun:test'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'
import { startApprovalStub, startForwardStub, type StubApproval, type StubForward } from './stubs'

let approval: StubApproval
let forward: StubForward
let client: Client

beforeAll(async () => {
  approval = await startApprovalStub()
  forward = await startForwardStub('deny-close')
  client = new Client({ name: 'test', version: '0.0.0' })
  await client.connect(
    new StdioClientTransport({
      command: process.execPath,
      args: ['run', 'src/index.ts'],
      cwd: new URL('..', import.meta.url).pathname,
      env: {
        PATH: process.env.PATH ?? '',
        BRAMBLE_APPROVAL_URL: approval.url,
        APPROVAL_DEMO_TOKEN: approval.token,
        BRAMBLE_AGENT_LABEL: 'agent-1',
        BRAMBLE_GATEWAY_LABEL: 'gateway-1',
        BRAMBLE_FORWARD_URL: forward.url,
        BRAMBLE_POLL_INTERVAL_MS: '20',
      },
    })
  )
})

afterAll(async () => {
  await client.close()
  await approval.close()
  await forward.close()
})

const text = (result: Awaited<ReturnType<Client['callTool']>>) =>
  (result.content as { type: string; text: string }[])[0].text

test('lists exactly the two tools', async () => {
  const { tools } = await client.listTools()
  expect(tools.map((tool) => tool.name).sort()).toEqual(['call_private_service', 'request_private_access'])
})

test('runs deny, request, approve and succeed over the MCP protocol', async () => {
  expect(text(await client.callTool({ name: 'call_private_service', arguments: { path: '/task' } }))).toStartWith(
    'denied: no grant'
  )

  const pending = client.callTool({
    name: 'request_private_access',
    arguments: { service: 'staging-api', reason: 'fetch the health task' },
  })
  await Bun.sleep(100)
  approval.setStatus('req-1', 'approved')
  forward.mode = 'allow'
  expect(text(await pending)).toStartWith('granted')

  expect(text(await client.callTool({ name: 'call_private_service', arguments: { path: '/task' } }))).toStartWith(
    'task result: '
  )
})
