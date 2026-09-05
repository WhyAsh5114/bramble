import { Hono } from 'hono'
import { SIDECAR_PORT } from './ens/config'
import { deviceRoute } from './routes/device'

const app = new Hono()

app.get('/health', (c) => c.json({ ok: true }))
app.route('/device', deviceRoute)

export default {
  port: SIDECAR_PORT,
  fetch: app.fetch,
}

console.log(`bramble sidecar listening on :${SIDECAR_PORT}`)
