import { withoutIdleTimeout } from '../../src/serve'

const delayMs = Number(process.argv[2])
const server = Bun.serve({
  port: 0,
  hostname: '127.0.0.1',
  fetch: withoutIdleTimeout(async () => {
    await new Promise((resolve) => setTimeout(resolve, delayMs))
    return new Response('ok')
  }),
})
console.log(server.port)
