type TimeoutServer = { timeout(request: Request, seconds: number): void }
type Handler = (request: Request, server: TimeoutServer) => Response | Promise<Response>

// Bun closes a request that has not answered within 10 s by default. Tor-backed
// ENS and payment calls legitimately take longer (21 s first request), so the
// server limit is lifted per request; the Go caller's per-operation budget
// (brambled/sidecar/client.go) is the bound that applies.
export function withoutIdleTimeout(handler: (request: Request) => Response | Promise<Response>): Handler {
  return (request, server) => {
    server.timeout(request, 0)
    return handler(request)
  }
}
