import { loadConfig } from './config.ts'
import { createProxyServer } from './server.ts'

const config = loadConfig()
const { server } = createProxyServer(config)

server.listen(config.port, config.host, () => {
  console.log(`rpc-proxy listening on ${config.host}:${config.port} (log: memory only, capacity ${config.logCapacity})`)
})

for (const signal of ['SIGINT', 'SIGTERM'] as const) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
