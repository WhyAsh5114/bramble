import http from 'node:http'
import type { AddressInfo } from 'node:net'

export interface Stub {
  url: string
  close(): Promise<void>
}

async function listen(server: http.Server): Promise<Stub> {
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address() as AddressInfo
  return {
    url: `http://127.0.0.1:${port}`,
    close: () =>
      new Promise<void>((resolve) => {
        server.closeAllConnections()
        server.close(() => resolve())
      }),
  }
}

export interface StubApproval extends Stub {
  token: string
  requests: Map<string, { id: string; status: string; body: Record<string, unknown> }>
  posts: number
  setStatus(id: string, status: string): void
}

export async function startApprovalStub(token = 'demo-token'): Promise<StubApproval> {
  const requests: StubApproval['requests'] = new Map()
  const stub = { token, requests, posts: 0 } as StubApproval
  const server = http.createServer((req, res) => {
    const send = (status: number, body: unknown) => {
      res.writeHead(status, { 'content-type': 'application/json' })
      res.end(JSON.stringify(body))
    }
    let raw = ''
    req.on('data', (chunk) => (raw += chunk))
    req.on('end', () => {
      if (req.method === 'POST') {
        if (req.headers.authorization !== `Bearer ${token}`) {
          return send(401, { error: { code: 'unauthorized', message: 'A valid demo bearer token is required.' } })
        }
        if (req.url === '/api/requests') {
          stub.posts++
          const id = `req-${requests.size + 1}`
          const body = JSON.parse(raw) as Record<string, unknown>
          requests.set(id, { id, status: 'pending', body })
          return send(201, { id, status: 'pending', ...body })
        }
      }
      const detail = /^\/api\/requests\/([^/]+)$/.exec(req.url ?? '')
      if (req.method === 'GET' && detail) {
        const found = requests.get(detail[1])
        return found
          ? send(200, { id: found.id, status: found.status, ...found.body })
          : send(404, { error: { code: 'request_not_found', message: 'Request not found.' } })
      }
      send(404, { error: { code: 'route_not_found', message: 'Route not found.' } })
    })
  })
  const base = await listen(server)
  return Object.assign(stub, base, {
    setStatus(id: string, status: string) {
      const found = requests.get(id)
      if (found) found.status = status
    },
  })
}

export type ForwardMode = 'allow' | 'deny-close' | 'deny-403'

export interface StubForward extends Stub {
  mode: ForwardMode
  paths: string[]
}

export async function startForwardStub(mode: ForwardMode): Promise<StubForward> {
  const stub = { mode, paths: [] as string[] } as StubForward
  const server = http.createServer((req, res) => {
    stub.paths.push(req.url ?? '')
    if (stub.mode === 'deny-close') return req.socket.destroy()
    if (stub.mode === 'deny-403') {
      res.writeHead(403)
      return res.end('DENY')
    }
    res.writeHead(200, { 'content-type': 'application/json' })
    res.end(JSON.stringify({ task: 'report-private-service-health', host: 'private-1', status: 'healthy' }))
  })
  return Object.assign(stub, await listen(server))
}
