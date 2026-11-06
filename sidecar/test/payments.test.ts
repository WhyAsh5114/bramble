import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { encodePaymentRequiredHeader } from '@x402/core/http'
import { PrivateKey } from '@x402/hedera'
import { payForDataRelaySession } from '../src/payments/datarelay'
import { paymentsRoute } from '../src/routes/payments'
import { stopTor } from '../src/tor'
import { startFakeTor } from './helpers/fake-tor'
import { installNetworkGuard, type NetworkGuard } from './helpers/network-guard'

describe('data-relay payment input policy', () => {
  it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1])(
    'rejects unsafe byte allotment %s before signing',
    async (bytes) => {
      await expect(payForDataRelaySession('https://relay.example', bytes)).rejects.toThrow(
        'data-relay session bytes must be a positive safe integer'
      )
    }
  )
})

describe('rendezvous payment rejection', () => {
  let guard: NetworkGuard
  beforeEach(() => {
    vi.stubEnv('HEDERA_CLIENT_ACCOUNT_ID', '0.0.1001')
    vi.stubEnv('HEDERA_CLIENT_PRIVATE_KEY', PrivateKey.generateECDSA().toStringRaw())
    vi.stubEnv('HEDERA_MAX_PAYMENT_ATOMIC', '100000')
    guard = installNetworkGuard(async () => {
      throw new Error('unexpected direct fetch')
    })
  })
  afterEach(async () => {
    guard.restore()
    vi.unstubAllEnvs()
    await stopTor()
  })

  async function rejectPayment(headers: Record<string, string>) {
    let attempts = 0
    const tor = await startFakeTor(async () => {
      if (++attempts === 1) {
        return new Response('{}', {
          status: 402,
          headers: {
            'PAYMENT-REQUIRED': encodePaymentRequiredHeader({
              x402Version: 2,
              resource: { url: 'https://relay.example/rendezvous-token' },
              accepts: [
                {
                  scheme: 'exact',
                  network: 'hedera:testnet',
                  asset: '0.0.429274',
                  amount: '10000',
                  payTo: '0.0.3003',
                  maxTimeoutSeconds: 60,
                  extra: { feePayer: '0.0.2002' },
                },
              ],
            }),
          },
        })
      }
      return new Response('{"error":"untrusted upstream detail"}', { status: 402, headers })
    })
    const response = await paymentsRoute.request('/rendezvous-token', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ sidecarUrl: 'https://relay.example' }),
    })
    expect(response.status).toBe(502)
    expect(tor.calls).toHaveLength(2)
    expect(tor.calls[1].init.headers).toHaveProperty('payment-signature')
    expect(guard.attempts).toEqual([])
    expect(guard.fetchCalls).toEqual([])
    return response.json()
  }

  it('reports a safe settlement reason even when the final body has no useful error', async () => {
    const headers = {
      'PAYMENT-RESPONSE': Buffer.from(
        JSON.stringify({
          success: false,
          errorReason: 'transaction_failed',
          transaction: 'untrusted transaction',
          network: 'hedera:testnet',
        })
      ).toString('base64'),
    }
    expect(await rejectPayment(headers)).toEqual({
      error: 'relay-sidecar returned HTTP 402 for /rendezvous-token (payment-response: transaction_failed)',
    })
  })

  it('reports an allowlisted verification rejection from the final challenge', async () => {
    expect(
      await rejectPayment({
        'PAYMENT-REQUIRED': Buffer.from(JSON.stringify({ error: 'insufficient_balance' })).toString('base64'),
      })
    ).toEqual({
      error: 'relay-sidecar returned HTTP 402 for /rendezvous-token (payment-required: insufficient_balance)',
    })
  })

  it.each([
    'not-json',
    Buffer.from('null').toString('base64'),
    Buffer.from(JSON.stringify({ errorReason: 'untrusted upstream detail' })).toString('base64'),
    Buffer.from(JSON.stringify({ errorReason: { nested: 'untrusted upstream detail' } })).toString('base64'),
  ])('suppresses malformed or unreviewed settlement metadata (%#)', async (header) => {
    expect(await rejectPayment({ 'PAYMENT-RESPONSE': header })).toEqual({
      error: 'relay-sidecar returned HTTP 402 for /rendezvous-token (payment-response: unknown)',
    })
  })

  it('keeps the HTTP error when no payment metadata exists', async () => {
    expect(await rejectPayment({})).toEqual({
      error: 'relay-sidecar returned HTTP 402 for /rendezvous-token',
    })
  })
})
