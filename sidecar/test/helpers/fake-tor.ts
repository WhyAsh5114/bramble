import { startTor, type TorFetchInit, type TorTransport } from '../../src/tor'

export type TorCall = { url: string; init: TorFetchInit; isolationKey: string | undefined }

export type FakeTor = { calls: TorCall[] }

type Handler = (url: string, init: TorFetchInit) => Promise<Response>

// Starts the shared Tor client on a transport that is ready at once and
// answers through `handler`, recording every request. Call stopTor() after.
export async function startFakeTor(handler: Handler): Promise<FakeTor> {
  const fake: FakeTor = { calls: [] }
  const transport: TorTransport = {
    ready: async () => {},
    fetch: async (url, init = {}) => {
      fake.calls.push({ url, init, isolationKey: init.anonOptions?.isolationKey })
      return handler(url, init)
    },
    close: () => {},
  }
  await startTor({ transport: () => transport, retryDelayMs: 0 })
  return fake
}

// Live-chain tests read the real RPC endpoint. This passes their requests
// straight to the global fetch so they keep testing ENS decoding, not Tor.
export function startDirectTor(): Promise<FakeTor> {
  return startFakeTor((url, init) => fetch(url, init as RequestInit))
}
