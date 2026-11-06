import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { loadConfig } from './config'
import { createServer } from './server'
import { createTools } from './tools'

try {
  const server = createServer(createTools(loadConfig()))
  await server.connect(new StdioServerTransport())
} catch (error) {
  console.error(`bramble-agent: ${error instanceof Error ? error.message : String(error)}`)
  process.exit(1)
}
