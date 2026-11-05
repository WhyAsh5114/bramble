// ETH-40 / W9a: does @x402/hedera's client signer reach the network outside
// the fetch it is wrapped by? The guard blocks and records the TCP, DNS, UDP,
// WebSocket, worker and child-process entry points listed in
// helpers/network-guard.ts (on Bun, worker and child-process named imports are
// not covered); only the stubbed global fetch may run. A pass means
// none of those paths was used, not that no packet could have been sent.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import net from 'node:net'
import dns from 'node:dns'
import dgram from 'node:dgram'
import childProcess from 'node:child_process'
import workerThreads from 'node:worker_threads'
import { decodePaymentSignatureHeader, encodePaymentRequiredHeader } from '@x402/core/http'
import type { PaymentRequired } from '@x402/core/types'
import { AccountBalanceQuery, AccountId, Client, PrivateKey, Transaction, TransferTransaction } from '@x402/hedera'
import { createClientHederaSigner } from '@x402/hedera'
import { ExactHederaScheme } from '@x402/hedera/exact/client'
import { payForDataRelaySession } from '../src/payments/datarelay'
import { stopTor } from '../src/tor'
import { payForRendezvousToken } from '../src/payments/client'
import { HEDERA_USDC_ASSET_ID } from '../src/payments/config'
import { startFakeTor, type TorCall } from './helpers/fake-tor'
import { activeNetworkHandles, installNetworkGuard, type NetworkGuard } from './helpers/network-guard'

const PAYER_ACCOUNT = '0.0.1001'
const FEE_PAYER = '0.0.2002'
const PAY_TO = '0.0.3003'
const AMOUNT = '1000'

const paymentRequired: PaymentRequired = {
  x402Version: 2,
  resource: { url: 'https://relay.example/rendezvous-token' },
  accepts: [
    {
      scheme: 'exact',
      network: 'hedera:testnet',
      amount: AMOUNT,
      asset: HEDERA_USDC_ASSET_ID,
      payTo: PAY_TO,
      maxTimeoutSeconds: 60,
      extra: { feePayer: FEE_PAYER },
    },
  ],
}

function relayStub(successBody: object) {
  let calls = 0
  return async (): Promise<Response> => {
    calls += 1
    if (calls === 1) {
      return new Response('{}', {
        status: 402,
        headers: { 'PAYMENT-REQUIRED': encodePaymentRequiredHeader(paymentRequired) },
      })
    }
    return new Response(JSON.stringify(successBody), { status: 200, headers: { 'content-type': 'application/json' } })
  }
}

// Every payment must be a signed TransferTransaction from the payer to payTo.
function expectSignedTransfer(signature: string, key: PrivateKey) {
  const payload = decodePaymentSignatureHeader(signature).payload as { transaction: string }
  const tx = Transaction.fromBytes(Buffer.from(payload.transaction, 'base64'))
  expect(tx).toBeInstanceOf(TransferTransaction)
  expect(key.publicKey.verifyTransaction(tx)).toBe(true)
  const transfers = (tx as TransferTransaction).tokenTransfers
  expect(transfers.get(HEDERA_USDC_ASSET_ID)?.get(PAYER_ACCOUNT)?.toString()).toBe(`-${AMOUNT}`)
  expect(transfers.get(HEDERA_USDC_ASSET_ID)?.get(PAY_TO)?.toString()).toBe(AMOUNT)
}

// Every request, including the paid retry, uses the 'pay' isolation key.
function expectPayKey(calls: TorCall[]) {
  const keys = calls.map((c) => c.isolationKey)
  expect(keys[0]).toMatch(/^pay:/)
  expect(new Set(keys).size).toBe(1)
}

describe('guard self-check (a guard that cannot fail proves nothing)', () => {
  let guard: NetworkGuard
  beforeEach(() => {
    guard = installNetworkGuard(async () => new Response('{}'))
  })
  afterEach(() => guard.restore())

  it('records a TCP connect, DNS lookup and UDP send', () => {
    expect(() => net.connect(9, '127.0.0.1')).toThrow('network-guard')
    expect(() => dns.lookup('example.invalid', () => {})).toThrow('network-guard')
    expect(() => dgram.createSocket('udp4').send('x', 9, '127.0.0.1')).toThrow('network-guard')
    expect(guard.attempts.map((a) => a.kind)).toEqual(['net.Socket.connect', 'dns.lookup', 'dgram.Socket.send'])
  })

  it('records the promise-based DNS Resolver', () => {
    const resolver = new dns.promises.Resolver({ timeout: 200, tries: 1 })
    expect(() => resolver.resolve4('guard-probe.invalid')).toThrow('network-guard')
    expect(guard.attempts.map((a) => a.kind)).toEqual(['dns.promises.Resolver.resolve4'])
  })

  it('records a worker thread and a child process', async () => {
    expect(() => new workerThreads.Worker('process.exit(0)', { eval: true })).toThrow('network-guard')
    expect(() => childProcess.execFile(process.execPath, ['-e', '0'], () => {})).toThrow('network-guard')
    expect(guard.attempts.map((a) => a.kind)).toEqual(['worker_threads.Worker', 'child_process.execFile'])
  })

  it.skipIf('Bun' in globalThis)('records worker and child-process named imports (Node only)', async () => {
    const { Worker } = await import('node:worker_threads')
    const { execFile } = await import('node:child_process')
    expect(() => new Worker('process.exit(0)', { eval: true })).toThrow('network-guard')
    expect(() => execFile(process.execPath, ['-e', '0'], () => {})).toThrow('network-guard')
  })

  it('records the Hedera SDK when it really queries a consensus node', async () => {
    const client = Client.forTestnet()
    try {
      await Promise.race([
        new AccountBalanceQuery().setAccountId(AccountId.fromString(PAYER_ACCOUNT)).setMaxAttempts(1).execute(client),
        new Promise((resolve) => setTimeout(resolve, 3000)),
      ]).catch(() => {})
    } finally {
      client.close()
    }
    expect(guard.attempts.length).toBeGreaterThan(0)
  })
})

describe('@x402/hedera client signer stays inside the wrapped fetch', () => {
  const key = PrivateKey.generateECDSA()
  const savedEnv = { ...process.env }
  let guard: NetworkGuard
  let handlesBefore: string[]

  beforeEach(() => {
    process.env.HEDERA_CLIENT_ACCOUNT_ID = PAYER_ACCOUNT
    process.env.HEDERA_CLIENT_PRIVATE_KEY = key.toStringRaw()
    handlesBefore = activeNetworkHandles()
  })
  afterEach(() => {
    guard?.restore()
    stopTor()
    process.env = { ...savedEnv }
  })

  it('createClientHederaSigner signs a transfer with no socket, DNS or UDP call', async () => {
    guard = installNetworkGuard(async () => new Response('{}'))
    const signer = createClientHederaSigner(PAYER_ACCOUNT, key, { network: 'hedera:testnet' })
    const payload = await new ExactHederaScheme(signer).createPaymentPayload(2, paymentRequired.accepts[0])
    const signature = Buffer.from(JSON.stringify({ x402Version: 2, payload: payload.payload })).toString('base64')
    expectSignedTransfer(signature, key)
    expect(guard.attempts).toEqual([])
    expect(guard.fetchCalls).toEqual([])
    expect(activeNetworkHandles()).toEqual(handlesBefore)
  })

  // Since W9 the payment HTTP goes through torFetch('pay'), so the stub sits
  // behind a fake Tor transport and the global fetch must stay untouched.
  it("payForRendezvousToken reaches the network only through torFetch('pay')", async () => {
    const tor = await startFakeTor(relayStub({ token: 'tok', expiresAt: 123 }))
    guard = installNetworkGuard(async () => new Response('{}'))
    const result = await payForRendezvousToken('https://relay.example')
    expect(result).toMatchObject({ token: 'tok', expiresAt: 123 })
    expect(tor.calls.map((c) => c.url)).toEqual([
      'https://relay.example/rendezvous-token',
      'https://relay.example/rendezvous-token',
    ])
    expectPayKey(tor.calls)
    expectSignedTransfer(tor.calls[1].init.headers!['payment-signature'], key)
    expect(guard.fetchCalls).toEqual([])
    expect(guard.attempts).toEqual([])
    expect(activeNetworkHandles()).toEqual(handlesBefore)
  })

  it("payForDataRelaySession reaches the network only through torFetch('pay')", async () => {
    const tor = await startFakeTor(relayStub({ sessionId: 's1', port: 4000, expiresAt: 456 }))
    guard = installNetworkGuard(async () => new Response('{}'))
    const result = await payForDataRelaySession('https://relay.example', 1024)
    expect(result).toMatchObject({ sessionId: 's1', port: 4000, expiresAt: 456 })
    expect(tor.calls.map((c) => c.url)).toEqual([
      'https://relay.example/data-relay-session?bytes=1024',
      'https://relay.example/data-relay-session?bytes=1024',
    ])
    expectPayKey(tor.calls)
    expectSignedTransfer(tor.calls[1].init.headers!['payment-signature'], key)
    expect(guard.fetchCalls).toEqual([])
    expect(guard.attempts).toEqual([])
    expect(activeNetworkHandles()).toEqual(handlesBefore)
  })
})
