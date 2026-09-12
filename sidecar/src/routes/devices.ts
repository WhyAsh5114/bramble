import { Hono } from 'hono'
import { listDeviceLabels } from '../ens/devices'

// Read-only device enumeration — the sidecar side of docs/adr/0010's
// full-mesh-by-default peer discovery. Same shape as relaysRoute
// (../relays.ts): a thin JSON wrapper around a chain-scanning function
// that already exists for a different caller (admincli's enroll.ts used
// listDeviceLabels first, for mesh-ip allocation).
export const devicesRoute = new Hono()

devicesRoute.get('/devices', async (c) => {
  try {
    const labels = await listDeviceLabels()
    return c.json({ labels })
  } catch (err) {
    return c.json({ error: err instanceof Error ? err.message : String(err) }, 502)
  }
})
