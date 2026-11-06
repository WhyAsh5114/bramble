// The production transport (real tor-js 0.4.4 TorClient and WASM) against a
// local server that accepts relay connections and never answers, so every
// bootstrap hangs. Only the dial destination is redirected; no Tor network.
// Regression for ETH-107: tor-js's own close() left a pending bootstrap's
// direct TCP sockets open, and a replacement client opened more alongside.
import net from 'node:net'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { getTorStatus, startTor, stopTor } from '../src/tor'
import { torJsTransport } from '../src/tor/transport'

let server: net.Server
let port = 0
// Server side of every accepted connection, by the client that dialed it.
const accepted: { owner: number; socket: net.Socket; closed: boolean }[] = []
let dialing = 0
// Client side of every dialed connection, by owner.
const dialed: { owner: number; socket: net.Socket }[] = []

beforeAll(async () => {
  server = net.createServer((socket) => {
    const item = { owner: dialing, socket, closed: false }
    accepted.push(item)
    // Read and discard, so the peer's FIN surfaces as 'end' and then 'close'.
    socket.resume()
    socket.on('error', () => {})
    socket.on('close', () => (item.closed = true))
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  port = (server.address() as net.AddressInfo).port
})

afterAll(async () => {
  for (const item of accepted) item.socket.destroy()
  await new Promise<void>((resolve) => server.close(() => resolve()))
})

afterEach(async () => {
  await stopTor()
  accepted.length = 0
  dialed.length = 0
})

// A dial that sends every relay connection to the blackhole and tags it with `owner`.
function blackholeDial(owner: number) {
  return () => {
    dialing = owner
    const socket = net.createConnection({ host: '127.0.0.1', port })
    dialed.push({ owner, socket })
    return socket
  }
}

const open = (owner: number) => accepted.filter((a) => a.owner === owner && !a.closed).length
// Client sockets of `owner` not yet destroyed.
const live = (owner: number) => dialed.filter((d) => d.owner === owner && !d.socket.destroyed).length
const of = (owner: number) => accepted.filter((a) => a.owner === owner).length

async function until(cond: () => boolean, ms = 10_000): Promise<void> {
  const deadline = Date.now() + ms
  while (!cond()) {
    if (Date.now() > deadline) throw new Error('condition not met in time')
    await new Promise((r) => setTimeout(r, 5))
  }
}

describe('torJsTransport', () => {
  it('closes every relay socket of a bootstrap still in progress, and dials no more', async () => {
    const t = await torJsTransport({ dial: blackholeDial(1) })
    // Unhandled otherwise: ready() rejects once the client is closed.
    t.ready().catch(() => {})
    await until(() => of(1) > 0)

    // Checked before awaiting: Arti closes blackholed sockets itself once its
    // handshake timeout fires, so only an immediate check shows close() did it.
    const closing = t.close()
    expect(live(1)).toBe(0)
    await closing
    await until(() => open(1) === 0, 2_000)

    const count = dialed.length
    await new Promise((r) => setTimeout(r, 500))
    expect(dialed.length).toBe(count)
    expect(open(1)).toBe(0)
  }, 20_000)

  it('keeps a timed-out client: the next attempt waits on its bootstrap and no second client dials', async () => {
    let clients = 0
    const factory = () => torJsTransport({ dial: blackholeDial(++clients) })

    await expect(startTor({ transport: factory, readyTimeoutMs: 500, retryDelayMs: 0 })).rejects.toThrow(
      'Tor not ready after'
    )
    expect(getTorStatus().state).toBe('failed')
    expect(of(1)).toBeGreaterThan(0)

    const again = startTor().catch(() => {})
    await new Promise((r) => setTimeout(r, 600))
    expect(clients).toBe(1)
    expect(of(2)).toBe(0)

    // stopTor tears down the pending bootstrap's sockets at once.
    const stopped = stopTor()
    expect(live(1)).toBe(0)
    await stopped
    await again
  }, 20_000)
})
