// The seam between the sidecar and tor-js. Production code gets a real
// TorClient; unit tests pass a fake that implements the same three methods
// (see test/tor.test.ts). Keeping the seam this narrow means tests never load
// the 1.9 MB Arti WASM binary or touch the network.
import net from 'node:net'

// Mirrors tor-js 0.4.4's FetchInit (dist/types.d.ts). Declared here, not
// imported, so the fake transport in tests does not depend on tor-js types.
export interface TorFetchInit {
  method?: string
  headers?: Record<string, string>
  body?: string | Uint8Array | ArrayBuffer | ReadableStream<Uint8Array>
  signal?: AbortSignal
  anonOptions?: { isolationKey?: string }
}

export interface TorTransport {
  /** Resolves once a guard is connected and the consensus is usable. */
  ready(): Promise<void>
  fetch(url: string, init?: TorFetchInit): Promise<Response>
  /**
   * Stops the client and every relay connection it opened, including those
   * of a bootstrap still in progress. A returned promise resolves once they
   * are closed; startTor waits for it before it creates another client.
   */
  close(): void | Promise<void>
}

export type TorTransportFactory = () => TorTransport | Promise<TorTransport>

export interface TorJsTransportOptions {
  /** Opens the TCP connection to a relay. Tests point it at a local server. */
  dial?: (host: string, port: number) => net.Socket
}

// close() waits this long for the socket 'close' events. destroy() is
// synchronous and the events follow on the next tick, so this cap only
// matters if the runtime never reports a close.
const SOCKET_CLOSE_WAIT_MS = 5_000

const defaultDial = (host: string, port: number) => net.createConnection({ host, port })

// "1.2.3.4:9001" or "[2001:db8::1]:9001".
function parseRelayAddress(target: string): { host: string; port: number } {
  const colon = target.lastIndexOf(':')
  const port = Number(target.slice(colon + 1))
  if (colon <= 0 || !Number.isInteger(port) || port <= 0 || port > 65_535) {
    throw new Error(`bad relay address: ${target}`)
  }
  return { host: target.slice(0, colon).replace(/^\[(.*)\]$/, '$1'), port }
}

// tor-js ships three entry points that differ only in how the WASM loads.
// The default (`tor-js`) downloads it from jsDelivr, unpkg or GitHub in the
// clear and caches it under ~/.local/share/tor-js-wasm, which would leak
// "this host runs tor-js" to a CDN and write local state. `tor-js/wasm-file`
// reads tor_js_bg.wasm from node_modules instead: no network, no writes.
//
// Storage is MemoryStorage for the same reason. The Node default is a
// filesystem store under ~/.local/share/tor-js; MemoryStorage keeps the
// consensus and microdescriptors in this process only, so a restart
// bootstraps from scratch (measured 51 s under Bun, 5 Nov 2026) and the
// node runtime path writes nothing to disk (pnpm check:no-local-state).
//
// Relay sockets: tor-js 0.4.4's TorClient.close() closes only KPS gateway
// tunnels. Its direct TCP sockets stay open, and a bootstrap still in
// progress keeps opening more (src/TorClient.ts close(), src/socketProvider.ts
// close(); Verified by the ETH-107 loopback probe). So this transport hands
// tor-js its own socket provider that dials every relay itself, holds each
// socket from the moment it is created, and on close() destroys them all and
// refuses new dials.
export async function torJsTransport(opts: TorJsTransportOptions = {}): Promise<TorTransport> {
  const { TorClient, ArtiSocket, ArtiSocketProvider, storage } = await import('tor-js/wasm-file')
  const dial = opts.dial ?? defaultDial
  // Each open socket, with a promise that resolves when it has closed.
  const sockets = new Map<net.Socket, Promise<void>>()
  let closed = false
  let closing: Promise<void> | null = null
  const closedError = () => new Error('Tor transport is closed')

  class OwnedSocketProvider extends ArtiSocketProvider {
    override async connect(target: string): Promise<InstanceType<typeof ArtiSocket>> {
      if (closed) throw closedError()
      const { host, port } = parseRelayAddress(target)
      const socket = dial(host, port)
      sockets.set(
        socket,
        new Promise<void>((resolve) =>
          socket.once('close', () => {
            sockets.delete(socket)
            resolve()
          })
        )
      )
      await new Promise<void>((resolve, reject) => {
        socket.once('connect', resolve)
        socket.once('error', reject)
        // destroy() without an error emits only 'close'.
        socket.once('close', () => reject(closedError()))
      })
      const wrapped = await ArtiSocket.fromNodeSocket(socket)
      if (closed) {
        socket.destroy()
        throw closedError()
      }
      return wrapped
    }

    // TorClient.close() calls this, but only once bootstrap has created its
    // provider; the transport's close() below covers the earlier window.
    override close(): void {
      super.close()
      for (const socket of sockets.keys()) socket.destroy()
    }
  }

  const provider = new OwnedSocketProvider()
  const client = new TorClient({ storage: new storage.MemoryStorage(), socketProvider: provider })
  return {
    ready: () => client.ready(),
    fetch: (url, init) => client.fetch(url, init),
    close() {
      if (closing !== null) return closing
      // From here on connect() refuses new dials. Sockets still connecting
      // are in `sockets` already, so they are destroyed with the rest.
      closed = true
      client.close()
      const pending = [...sockets.values()]
      provider.close()
      closing = (async () => {
        let timer: ReturnType<typeof setTimeout> | undefined
        await Promise.race([
          Promise.all(pending),
          new Promise<void>((resolve) => (timer = setTimeout(resolve, SOCKET_CLOSE_WAIT_MS))),
        ])
        clearTimeout(timer)
      })()
      return closing
    },
  }
}

export const createTorJsTransport: TorTransportFactory = () => torJsTransport()
