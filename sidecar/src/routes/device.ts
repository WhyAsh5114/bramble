import { Hono } from 'hono'
import { resolveDevice } from '../ens/client'

export const deviceRoute = new Hono()

deviceRoute.get('/:label', async (c) => {
  const label = c.req.param('label')
  try {
    const record = await resolveDevice(label)
    return c.json(record)
  } catch (err) {
    return c.json({ error: err instanceof Error ? err.message : String(err) }, 502)
  }
})
