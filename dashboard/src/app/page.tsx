'use client'

import Link from 'next/link'
import { usePolling } from '@/hooks/use-polling'
import { MonoValue } from '@/components/mono-value'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { ENS_EXPLORER_URL, SEPOLIA_ETHERSCAN_URL } from '@/lib/config'
import { activityTone } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ActivityEvent, Relays, SidecarHealth } from '@/lib/types'

async function fetchHealth(): Promise<SidecarHealth> {
  const res = await fetch('/api/sidecar/health', { cache: 'no-store' })
  if (!res.ok) throw new Error(`sidecar /health returned ${res.status}`)
  return res.json()
}

async function fetchConfig(): Promise<{ deviceLabels: string[] }> {
  const res = await fetch('/api/config', { cache: 'no-store' })
  if (!res.ok) throw new Error(`config returned ${res.status}`)
  return res.json()
}

async function fetchRelays(): Promise<Relays> {
  const res = await fetch('/api/sidecar/relays', { cache: 'no-store' })
  if (!res.ok) throw new Error(`sidecar /relays returned ${res.status}`)
  return res.json()
}

async function fetchRecentActivity(): Promise<ActivityEvent[]> {
  const res = await fetch('/api/brambled/activity', { cache: 'no-store' })
  if (!res.ok) return []
  const events = (await res.json()) as ActivityEvent[]
  return events.slice(-5).reverse()
}

export default function OverviewPage() {
  const health = usePolling(fetchHealth, 10_000)
  const config = usePolling(fetchConfig, 30_000)
  const relays = usePolling(fetchRelays, 10_000)
  const activity = usePolling(fetchRecentActivity, 3_000)

  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Tailnet</h1>
        <p className="text-sm text-muted-foreground">
          A live, read-only view over one bramble tailnet, sourced entirely from on-chain state via this node&apos;s
          sidecar.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <StatTile label="Devices watched" href="/devices">
          {config.data ? (
            <span className="text-2xl font-medium text-foreground">{config.data.deviceLabels.length}</span>
          ) : (
            <Skeleton className="h-8 w-10" />
          )}
        </StatTile>

        <StatTile label="Relays discovered" href="/relays">
          {relays.data ? (
            <span className="text-2xl font-medium text-foreground">
              {relays.data.rendezvous.length + relays.data.dataRelays.length}
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                {relays.data.rendezvous.length} rendezvous · {relays.data.dataRelays.length} data
              </span>
            </span>
          ) : (
            <Skeleton className="h-8 w-24" />
          )}
        </StatTile>

        <StatTile label="Tailnet">
          {health.data ? (
            <span className="truncate font-mono text-lg text-foreground">{health.data.tailnetName}</span>
          ) : health.error ? (
            <span className="text-sm text-destructive">sidecar unreachable</span>
          ) : (
            <Skeleton className="h-7 w-32" />
          )}
        </StatTile>
      </div>

      {health.data && (
        <div className="-mt-4 flex items-center gap-1.5 text-xs text-muted-foreground">
          registry
          <MonoValue
            value={health.data.tailnetRegistry}
            display={health.data.tailnetRegistry}
            href={`${SEPOLIA_ETHERSCAN_URL}/address/${health.data.tailnetRegistry}`}
          />
        </div>
      )}

      <Separator />

      <div className="flex flex-col gap-3">
        <div className="flex items-baseline justify-between">
          <h2 className="text-sm text-muted-foreground">Recent activity</h2>
          <Link
            href="/activity"
            className="text-xs text-muted-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
          >
            view all →
          </Link>
        </div>
        {!activity.data && !activity.error && <Skeleton className="h-20 w-full" />}
        {activity.data && activity.data.length === 0 && (
          <p className="text-sm text-muted-foreground">No activity yet.</p>
        )}
        {activity.data && activity.data.length > 0 && (
          <ul className="flex flex-col gap-1.5">
            {activity.data.map((e, i) => (
              <li
                key={`${e.time}-${i}`}
                className={cn(
                  'flex items-baseline gap-3 border-l-2 py-0.5 pl-3 text-sm',
                  activityTone(e) === 'negative' && 'border-destructive',
                  activityTone(e) === 'positive' && 'border-positive',
                  activityTone(e) === 'neutral' && 'border-border'
                )}
              >
                <span className="shrink-0 font-mono text-xs text-muted-foreground">
                  {new Date(e.time).toLocaleTimeString()}
                </span>
                <span className="shrink-0 font-mono text-sm text-foreground">{e.label}</span>
                <span className="truncate text-muted-foreground">{e.message}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <Separator />

      <div className="flex flex-col gap-2">
        <h2 className="text-sm text-muted-foreground">On-chain tools</h2>
        <div className="flex flex-col gap-1">
          <a
            href={ENS_EXPLORER_URL}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
          >
            Hackathon ENSv2 explorer ↗
          </a>
          <a
            href={SEPOLIA_ETHERSCAN_URL}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
          >
            Sepolia Etherscan ↗
          </a>
        </div>
      </div>
    </div>
  )
}

function StatTile({ label, href, children }: { label: string; href?: string; children: React.ReactNode }) {
  const content = (
    <div className="flex h-full flex-col justify-between gap-2 rounded-lg border bg-card p-4 transition-colors hover:border-foreground/20">
      <span className="text-xs text-muted-foreground">{label}</span>
      {children}
    </div>
  )
  if (!href) return content
  return (
    <Link href={href} className="block">
      {content}
    </Link>
  )
}
