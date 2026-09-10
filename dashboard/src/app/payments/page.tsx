'use client'

import { usePolling } from '@/hooks/use-polling'
import { MonoValue } from '@/components/mono-value'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusDot } from '@/components/status-dot'
import type { HederaPayment } from '@/lib/types'

async function fetchPayments(): Promise<HederaPayment[]> {
  const res = await fetch('/api/hedera/payments', { cache: 'no-store' })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `HTTP ${res.status}`)
  }
  return res.json()
}

function relativeTime(consensusTimestamp: string): string {
  const ms = Number(consensusTimestamp) * 1000
  const deltaMs = Date.now() - ms
  const secs = Math.floor(deltaMs / 1000)
  if (secs < 5) return 'just now'
  if (secs < 60) return `${secs}s ago`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

export default function PaymentsPage() {
  const payments = usePolling(fetchPayments, 10_000)
  const total = payments.data?.reduce((sum, p) => sum + Number(p.amount), 0) ?? 0

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Payments</h1>
        <p className="text-sm text-muted-foreground">
          Real testnet USDC settlements, read directly from Hedera&apos;s public mirror node — the same source used to
          independently confirm every payment gate in this project, not a log this dashboard is trusted to report
          honestly. Every relay purchase (a rendezvous token, or a data-relay byte allotment) pays through x402 and
          Blocky402; both land here.
        </p>
      </div>

      {!payments.data && !payments.error && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-14 w-full rounded-lg" />
          <Skeleton className="h-14 w-full rounded-lg" />
          <Skeleton className="h-14 w-full rounded-lg" />
        </div>
      )}

      {payments.error && (
        <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          {payments.error.includes('not configured') ? (
            <>
              No account configured — set <code className="font-mono text-foreground">HEDERA_WATCH_ACCOUNT_IDS</code> in
              the dashboard&apos;s environment to a relay operator&apos;s Hedera account id (e.g. the{' '}
              <code className="font-mono text-foreground">-payee</code> value passed to <code>relay -meter</code>).
            </>
          ) : (
            `mirror node unreachable — ${payments.error}`
          )}
        </div>
      )}

      {payments.data && payments.data.length === 0 && (
        <p className="text-sm text-muted-foreground">No settlements yet for the watched account(s).</p>
      )}

      {payments.data && payments.data.length > 0 && (
        <>
          <div className="flex items-center gap-2 rounded-lg border bg-card p-4">
            <StatusDot tone="positive" pulse />
            <span className="text-sm text-muted-foreground">
              <span className="font-mono text-base font-medium text-foreground">{total.toFixed(2)} USDC</span> settled
              across {payments.data.length} recent transaction{payments.data.length === 1 ? '' : 's'}
            </span>
          </div>

          <ul className="flex flex-col gap-2">
            {payments.data.map((p) => (
              <li
                key={p.transactionId}
                className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-lg border bg-card px-4 py-3 transition-colors hover:border-foreground/20"
              >
                <div className="flex items-center gap-3">
                  <span className="font-mono text-sm font-medium text-foreground">{p.amount} USDC</span>
                  <span className="text-xs text-muted-foreground">
                    <span className="font-mono">{p.from}</span> → <span className="font-mono">{p.to}</span>
                  </span>
                </div>
                <div className="flex items-center gap-3">
                  <span className="text-xs text-muted-foreground">{relativeTime(p.consensusTimestamp)}</span>
                  <MonoValue value={p.transactionId} display="view on HashScan ↗" href={p.hashscanUrl} />
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  )
}
