# Capture the provider view for beat 6

Run this recipe from the repository root on one development machine. It starts
the real logging RPC proxy, the production ENS read client and a local pair of
userspace WireGuard nodes. It requires Node 24, Bun, Go, pnpm and installed
workspace dependencies. It needs no cloud nodes, signing credentials or network
services. Install dependencies before going offline:

```sh
pnpm install --frozen-lockfile
```

**Verified, 6 November 2026 (base `52a35cb`):** the local capture records 49
`eth_call` requests per phase, with gateway decisions `denied`, `allowed`,
`denied` in each phase. Exactly two backend calls succeed across both phases.
The run takes 8–12 s wall clock in two runs, mostly the Go test build; the
script reports under 1 s of capture time. The capture uses an in-memory chain
and a fake Tor transport. Both phases log `127.0.0.1`. Neither phase shows a Tor
exit or source-IP anonymity. The request count depends on poll timing; treat 49
as one observation, not a constant.

**Verified, same run:** the private service name (`staging-api`) and the
backend response body (`private task over WireGuard`) do not occur in the
captured RPC log. The logged `params` are hex-encoded ABI (application binary
interface) calls. Decoding them recovers the device labels (`agent-1`,
`gateway-1`), the tailnet label (`fixture`) and the record keys (`pubkey`,
`acl-granters`, `mesh-ip`, `revoked`). Step 4 shows how.

**Read from source:** the provider sees source IP, time, method and RPC
parameters. Tor changes the source-IP exposure, not the query contents. Do not
say “the provider sees no identity”: it sees device and network names. This
capture uses fictional names and no real-person identity.

## Run the capture

1. Extract the TypeScript block below into an ignored scratch directory:

   ```sh
   mkdir -p .tmp/eth-296
   python3 - <<'PYTHON'
   from pathlib import Path
   doc = Path('docs/demo/beat6-provider-view.md').read_text()
   code = doc.split('```typescript\n', 1)[1].split('\n```', 1)[0]
   Path('.tmp/eth-296/capture.ts').write_text(code + '\n')
   PYTHON
   ```

2. Run it. Redirect only the fixture capture, never an environment dump:

   ```sh
   bun run .tmp/eth-296/capture.ts > .tmp/eth-296/provider.json
   ```

   The script supplies all RPC configuration explicitly. The fake upstream
   implements the read ABI (application binary interface), not a real chain.
   The script switches the real `torFetch('ens')` wrapper to a fake transport
   that forwards to loopback. It checks the `ens` isolation key but never
   bootstraps Tor. It reuses `TestAccessFlowFixture` for the real WireGuard pair.
   Grants and revokes change only fixture records. No signer sends a transaction.

3. Display the captured metadata without the long query bytes:

   ```sh
   python3 - <<'PYTHON'
   import json
   from pathlib import Path
   capture = json.loads(Path('.tmp/eth-296/provider.json').read_text())
   print(capture['label'], capture['capturedAt'])
   for phase in capture['phases']:
       print(phase['phase'], phase['decisions'], 'requests:', len(phase['entries']))
       print('seq  time                      sourceIp   method       params chars')
       for entry in phase['entries'][:3]:
           print(entry['seq'], entry['time'], entry['sourceIp'], entry['method'], len(entry['params']))
   print('backend calls:', capture['backendCalls'])
   PYTHON
   ```

   The display omits `params`; the raw capture retains them. This display filter
   does not hide queries from a provider. The script checks the raw log for the
   backend response and private service name, checks every source IP and method,
   and fails on an incorrect gateway decision or a dropped log entry. A timeout
   without the gateway's explicit `no matching granted digest` is a failure.
   Processes and listeners stop when the script exits. Each run creates a fresh
   in-memory log; the proxy has no reset endpoint.

4. Decode what the provider can read in the `params`:

   ```sh
   python3 - <<'PYTHON'
   import collections, json, re
   from pathlib import Path
   capture = json.loads(Path('.tmp/eth-296/provider.json').read_text())
   for phase in capture['phases']:
       seen = collections.Counter()
       for entry in phase['entries']:
           data = json.loads(entry['params'])[0]['data']
           for word in re.findall(rb'[a-z0-9.-]{4,}', bytes.fromhex(data[2:])):
               seen[word.decode()] += 1
       print(phase['phase'], dict(sorted(seen.items())))
   PYTHON
   ```

   Each phase lists the device labels, the tailnet label and the record keys
   with their counts. The decode is a regular expression over the call data. It
   shows what any reader of the log can recover; it is not a complete parse.

## Capture script

Save this block through step 1. Keep it under `.tmp/eth-296/` so its relative
imports resolve. Do not run it from `/tmp`.

```typescript
import assert from 'node:assert/strict'
import { once } from 'node:events'
import { createServer } from 'node:http'
import { createProxyServer } from '../../rpc-proxy/src/server.ts'
import { loadConfig } from '../../rpc-proxy/src/config.ts'
import { x25519 } from '../../agent-mcp/node_modules/@noble/curves/ed25519.js'
import { aclDigestECDH } from '../../admincli/src/roles.ts'
import { decodeFunctionData, encodeFunctionResult, parseAbi, toHex } from '../../sidecar/node_modules/viem/index.ts'

const started = performance.now()
const resolver = '0x0000000000000000000000000000000000000001'
const abi = parseAbi([
  'function resolveWithGateways(bytes name, bytes data, string[] gateways) view returns (bytes, address)',
  'function text(bytes32 node, string key) view returns (string)',
  'function findResolver(bytes name) view returns (address, bytes32, uint256)',
  'function getState(uint256 id) view returns (uint8, uint64, address, uint256, uint256)',
])
const records = new Map<string, string>()
// This identity exists only in memory for this local fixture. Never print it.
const granter = x25519.utils.randomPrivateKey()
records.set('granter|pubkey', Buffer.from(x25519.getPublicKey(granter)).toString('hex'))
records.set('gateway-1|acl-granters', 'granter')
const dnsLabel = (hex: string) => {
  const bytes = Buffer.from(hex.slice(2), 'hex')
  return bytes.subarray(1, 1 + bytes[0]!).toString()
}
const upstream = createServer(async (req, res) => {
  const parts = []
  for await (const part of req) parts.push(part)
  const call = JSON.parse(Buffer.concat(parts).toString())
  assert.equal(call.method, 'eth_call')
  const decoded = decodeFunctionData({ abi, data: call.params[0].data })
  const args = decoded.args as readonly any[]
  let result: string
  if (decoded.functionName === 'resolveWithGateways') {
    const text = decodeFunctionData({ abi, data: args[1] })
    const key = text.args![1]
    const value = records.get(`${dnsLabel(args[0])}|${key}`) ?? ''
    result = encodeFunctionResult({ abi, functionName: 'resolveWithGateways', result: [
      encodeFunctionResult({ abi, functionName: 'text', result: value }), resolver,
    ] })
  } else if (decoded.functionName === 'findResolver') {
    result = encodeFunctionResult({ abi, functionName: 'findResolver', result: [resolver, toHex(0, { size: 32 }), 0n] })
  } else {
    assert.equal(decoded.functionName, 'getState')
    result = encodeFunctionResult({ abi, functionName: 'getState', result: [0, 4102444800n, resolver, 1n, 0n] })
  }
  res.writeHead(200, { 'content-type': 'application/json' })
  res.end(JSON.stringify({ jsonrpc: '2.0', id: call.id, result }))
})
upstream.listen(0, '127.0.0.1')
await once(upstream, 'listening')
const port = (server: any) => server.address().port
const { server: proxy } = createProxyServer(loadConfig({
  UPSTREAM_RPC_URL: `http://127.0.0.1:${port(upstream)}`,
  TRUSTED_PROXY_HOPS: '0', RATE_LIMIT_PER_MINUTE: '0', MAX_LOGGED_PARAMS_CHARS: '8192',
}))
proxy.listen(0, '127.0.0.1')
await once(proxy, 'listening')
const proxyUrl = `http://127.0.0.1:${port(proxy)}`
process.env.SEPOLIA_RPC_URL = proxyUrl
process.env.BRAMBLE_TAILNET_NAME = 'fixture.eth'
process.env.BRAMBLE_TAILNET_REGISTRY = resolver
const { createEnsClient, resolveDevice } = await import('../../sidecar/src/ens/client.ts')
const { startTor, stopTor, torFetch } = await import('../../sidecar/src/tor/index.ts')
let transportCalls = 0
await startTor({ retryDelayMs: 0, transport: () => ({
  ready: async () => {}, close: () => {},
  fetch: async (url, init = {}) => {
    assert.match(init.anonOptions?.isolationKey ?? '', /^ens:/)
    transportCalls++
    return fetch(url, { method: init.method, headers: init.headers, body: init.body as any })
  },
}) })
let client = createEnsClient(fetch)
const localSidecar = Bun.serve({ hostname: '127.0.0.1', port: 0, async fetch(req) {
  const path = new URL(req.url).pathname
  if (!path.startsWith('/devices/')) return new Response('', { status: 404 })
  return Response.json(await resolveDevice(path.slice('/devices/'.length), client))
} })
const child = Bun.spawn(['go', 'test', './gateway', '-run', '^TestAccessFlowFixture$', '-count=1', '-v', '-timeout=50s'], {
  cwd: 'brambled', env: { ...process.env, BRAMBLE_ACCESS_FLOW_RESOLVER_URL: localSidecar.url.origin },
  stdout: 'pipe', stderr: 'pipe',
})
const stderr = new Response(child.stderr).text()
let fixture: any
let output = ''
const ready = Promise.withResolvers<any>()
const drain = (async () => {
  for await (const chunk of child.stdout) {
    output += new TextDecoder().decode(chunk)
    const match = /^ACCESS_FLOW_READY ([^\n]+)/m.exec(output)
    if (match) ready.resolve(JSON.parse(match[1]!))
  }
  ready.reject(new Error('local pair exited before ready'))
})()
let startupTimer: ReturnType<typeof setTimeout>
const timeout = new Promise<never>((_, reject) => {
  startupTimer = setTimeout(() => reject(new Error('pair startup timeout')), 30_000)
  startupTimer.unref()
})
const report: any = { label: 'LOCAL FIXTURE; fake chain; fake Tor; no cloud', capturedAt: new Date().toISOString(), phases: [] }
try {
  fixture = await Promise.race([ready.promise, timeout])
  clearTimeout(startupTimer!)
  records.set('agent-1|pubkey', fixture.agentPub)
  records.set('gateway-1|pubkey', fixture.gatewayPub)
  const digest = aclDigestECDH(Buffer.from(granter).toString('hex'), fixture.gatewayPub, fixture.agentPub, 'staging-api')
  let since = 0
  for (const phase of ['direct-local', 'fake-tor-local']) {
    client = createEnsClient(phase === 'direct-local' ? fetch : torFetch('ens'))
    const observations = []
    for (const allowed of [false, true, false]) {
      records.set('agent-1|acl', allowed ? digest : '')
      try {
        const response = await fetch(`${fixture.forwardUrl}/task`, { signal: AbortSignal.timeout(5000) })
        assert.equal(await response.text(), 'private task over WireGuard')
        assert.equal(allowed, true)
      } catch (error) {
        if (allowed) throw error
      }
      const observation = await (await fetch(fixture.controlUrl)).json() as any
      assert.equal(observation.decisions.at(-1).Allowed, allowed)
      if (allowed) assert.match(observation.decisions.at(-1).Detail, /^proxying to 127\.0\.0\.1:/)
      else assert.equal(observation.decisions.at(-1).Detail, 'no matching granted digest')
      observations.push(allowed ? 'allowed' : 'denied')
    }
    const log = await (await fetch(`${proxyUrl}/log?since=${since}`)).json() as any
    since = log.lastSeq
    assert.equal(log.dropped, 0)
    assert.ok(log.entries.length > 0)
    assert.ok(log.entries.every((entry: any) => entry.sourceIp === '127.0.0.1' && entry.method === 'eth_call'))
    const serialized = JSON.stringify(log)
    assert.ok(!serialized.includes('private task over WireGuard'))
    assert.ok(!serialized.includes('staging-api'))
    report.phases.push({ phase, decisions: observations, ...log })
  }
  assert.ok(transportCalls > 0)
  report.fakeTorRequests = transportCalls
  const observation = await (await fetch(fixture.controlUrl)).json() as any
  assert.deepEqual(observation.backendPaths, ['/task', '/task'])
  report.backendCalls = observation.backendPaths.length
  report.elapsedSeconds = Number(((performance.now() - started) / 1000).toFixed(2))
  console.log(JSON.stringify(report, null, 2))
} finally {
  clearTimeout(startupTimer!)
  if (fixture) await fetch(`${fixture.controlUrl}/stop`, { method: 'POST' }).catch(() => {})
  else child.kill()
  const code = await child.exited
  await drain
  await stderr
  await stopTor()
  localSidecar.stop(true)
  proxy.closeAllConnections()
  upstream.closeAllConnections()
  await Promise.all([new Promise<void>((done) => proxy.close(() => done())), new Promise<void>((done) => upstream.close(() => done()))])
  assert.equal(code, 0, 'local WireGuard pair failed')
}

```

## Present the comparison in 35 seconds

Use the 2:00–2:35 slot from handoff section 4. These are presentation budgets,
not measured system timings.

| Time | Show and say |
| --- | --- |
| 0–10 s | Show `direct-local`: “This local fixture logs the caller address and the ENS query.” |
| 10–20 s | Show `fake-tor-local`: “The Tor wrapper preserves those queries. This transport is simulated, so both addresses remain loopback.” |
| 20–30 s | Show the two successful backend calls and absence checks: “The private service payload follows WireGuard; it is absent from this RPC log. ENS names and timing remain visible.” |
| 30–35 s | “A real Tor exit comparison is separate evidence. This offline run does not establish it.” |

**Open:** a before/after capture using the same real node through a reachable
HTTPS proxy and real Tor, exit-address validation, any observed exit rotation,
and payment-provider visibility. A real circuit can reuse an exit; different
exits are not guaranteed per poll or per isolation key. Do not relabel loopback
addresses or inject `X-Forwarded-For` to manufacture exits. The current proxy
configuration ignores that header (`TRUSTED_PROXY_HOPS=0`).

For production behavior, see the [proxy reference](../../rpc-proxy/README.md),
[Tor transport](../../sidecar/src/tor/index.ts),
[ENS client](../../sidecar/src/ens/client.ts) and
[local pair fixture](../../brambled/gateway/access_flow_test.go).
WireGuard traffic and STUN (Session Traversal Utilities for NAT) stay outside
Tor. Relays and STUN servers can see peer addresses. Hiding the control-plane
source IP does not make traffic analysis or identity inference impossible.

If the cloud demo fails, use this capture as clip F of the [fallback plan](fallback.md).
