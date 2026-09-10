import { Hono } from 'hono'
import { SIDECAR_PORT, relayRegistry, tailnetName, tailnetRegistry } from './ens/config'
import { deviceRoute } from './routes/device'
import { paymentsRoute } from './routes/payments'
import { relaysRoute } from './routes/relays'

const app = new Hono()

// Identity is included so brambled/sidecar.Manager's health check can tell
// "my sidecar came up" from "some other process (a stray sidecar from
// another node, or anything else) is answering on this port" — a fixed port
// with a bare ok:true health check can't distinguish those, which is a
// silent-wrong-tailnet landmine on any host running more than one node.
app.get('/health', (c) =>
  c.json({
    ok: true,
    tailnetName: tailnetName(),
    tailnetRegistry: tailnetRegistry(),
    // Distinct, optional field -- a tailnet with no relay registry
    // configured just omits it, same "absent means not configured, not an
    // error" convention as resolveRelays() itself.
    relayRegistry: relayRegistry() ?? null,
  })
)
app.route('/device', deviceRoute)
app.route('/', paymentsRoute)
app.route('/', relaysRoute)

export default {
  port: SIDECAR_PORT,
  // Bind to loopback only — this sidecar serves one local node's brambled
  // process, never other hosts. Bun's default bind (all interfaces) would
  // otherwise expose the read endpoint to the network for no reason.
  hostname: '127.0.0.1',
  fetch: app.fetch,
}

console.log(`bramble sidecar listening on 127.0.0.1:${SIDECAR_PORT}`)
