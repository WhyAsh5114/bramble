import { Hono } from 'hono'
import { resolveRelays } from '../ens/relays'

export const relaysRoute = new Hono()

relaysRoute.get('/relays', async (c) => {
  try {
    const relays = await resolveRelays()
    return c.json(relays)
  } catch (err) {
    return c.json({ error: err instanceof Error ? err.message : String(err) }, 502)
  }
})
