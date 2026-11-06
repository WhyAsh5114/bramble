'use client'

import { useEffect, useState } from 'react'
import type { PaymentsFeed } from '../lib/payments'

function usdc(atomic: string) {
  const amount = BigInt(atomic)
  return `${amount / BigInt(1_000_000)}.${(amount % BigInt(1_000_000)).toString().padStart(6, '0')} USDC`
}

export function PaymentsPanel() {
  const [feed, setFeed] = useState<PaymentsFeed | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    let active = true
    let timer: ReturnType<typeof setTimeout>
    const controller = new AbortController()
    async function refresh() {
      try {
        const response = await fetch('/api/payments', { cache: 'no-store', signal: controller.signal })
        const body = await response.json().catch(() => {
          throw new Error('Payments unavailable. Retry shortly.')
        })
        if (!response.ok) throw new Error(body.error?.message ?? 'Payments unavailable. Retry shortly.')
        if (active) {
          setFeed(body)
          setError(null)
        }
      } catch (failure) {
        if (active) setError(failure instanceof Error ? failure.message : 'Payments unavailable.')
      } finally {
        if (active) timer = setTimeout(refresh, 2_000)
      }
    }
    void refresh()
    return () => {
      active = false
      clearTimeout(timer)
      controller.abort()
    }
  }, [])

  return (
    <section aria-labelledby="payments-title" className="payments-panel">
      <h1 id="payments-title">Agent payments</h1>
      <p>Hedera testnet · USDC · Refreshes every 2 seconds</p>
      <p>
        Attribution uses the operator’s dedicated payer mapping. Spend includes all outgoing USDC in the reporting
        window.
      </p>
      {error && (
        <p role="alert">
          {error} {feed && 'The values below are from the last successful refresh.'}
        </p>
      )}
      {!feed && !error && <p role="status">Loading mirror-node settlements…</p>}
      {feed && (
        <p>
          Last updated: <time dateTime={feed.observedAt}>{new Date(feed.observedAt).toLocaleTimeString()}</time>
          {error && ' · Stale'}
        </p>
      )}
      {feed?.agents.map((agent) => (
        <article key={agent.agentLabel}>
          <h2>{agent.agentLabel}</h2>
          <dl>
            <div>
              <dt>Payer account</dt>
              <dd>
                <code>{agent.payerAccountId}</code>
              </dd>
            </div>
            <div>
              <dt>Configured per-payment cap</dt>
              <dd>{usdc(agent.perPaymentCapAtomic)}</dd>
            </div>
            <div>
              <dt>Demo budget (advisory)</dt>
              <dd>{usdc(agent.budgetAtomic)}</dd>
            </div>
            <div>
              <dt>Total spent{error ? ' (stale)' : ''}</dt>
              <dd>{usdc(agent.spentAtomic)}</dd>
            </div>
            <div>
              <dt>Remaining budget{error ? ' (stale)' : ''}</dt>
              <dd>{usdc(agent.remainingAtomic)}</dd>
            </div>
          </dl>
          <p>
            Window starts at Hedera consensus time <code>{agent.since}</code>. The cap is approval-server configuration,
            not read from the sidecar. Confirm it matches the deployed sidecar. The demo budget does not stop payments.
          </p>
          {BigInt(agent.spentAtomic) > BigInt(agent.budgetAtomic) && <p role="status">Demo budget exceeded.</p>}
          {agent.payments.length === 0 ? (
            <p>No outgoing USDC settlements in this window. Relay connection attempts trigger payments.</p>
          ) : (
            <ul aria-label={`${agent.agentLabel} settlements`}>
              {agent.payments.map((payment) => (
                <li key={payment.consensusTimestamp}>
                  <span>{usdc(payment.amountAtomic)}</span>
                  {' · '}
                  <a href={payment.hashscanUrl} target="_blank" rel="noreferrer">
                    View settlement on HashScan
                  </a>
                  <p>
                    <code>{payment.transactionId}</code>
                  </p>
                </li>
              ))}
            </ul>
          )}
        </article>
      ))}
    </section>
  )
}
