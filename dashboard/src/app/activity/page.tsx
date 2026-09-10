'use client'

import { usePolling } from '@/hooks/use-polling'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusText } from '@/components/status-text'
import { cn } from '@/lib/utils'
import type { ActivityEvent } from '@/lib/types'

async function fetchActivity(): Promise<ActivityEvent[]> {
  const res = await fetch('/api/brambled/activity', { cache: 'no-store' })
  if (!res.ok) throw new Error(`brambled status /activity returned ${res.status}`)
  return res.json()
}

function isDenied(e: ActivityEvent): boolean {
  return e.kind === 'gateway' && e.message.startsWith('denied')
}

function isAuthorizedFalse(e: ActivityEvent): boolean {
  return e.kind === 'admission' && (e.message.startsWith('authorized=false') || e.message.startsWith('resolve error'))
}

export default function ActivityPage() {
  const activity = usePolling(fetchActivity, 3_000)
  const events = activity.data ? [...activity.data].reverse() : null

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Activity</h1>
        <p className="text-sm text-muted-foreground">
          Live admission decisions and gateway CONNECT outcomes from this node&apos;s own runtime — not ENS state, what
          actually happened at the WireGuard/gateway boundary.
        </p>
      </div>

      {!events && !activity.error && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-6 w-full" />
          <Skeleton className="h-6 w-full" />
          <Skeleton className="h-6 w-full" />
        </div>
      )}

      {events && events.length === 0 && <p className="text-sm text-muted-foreground">No activity yet.</p>}

      {events && events.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {events.map((e, i) => {
            const negative = isDenied(e) || isAuthorizedFalse(e)
            return (
              <li key={`${e.time}-${i}`} className="flex items-baseline gap-3 border-b py-1.5 text-sm last:border-b-0">
                <span className="shrink-0 font-mono text-xs text-muted-foreground">
                  {new Date(e.time).toLocaleTimeString()}
                </span>
                <span className="shrink-0 font-mono text-xs text-muted-foreground">[{e.kind}]</span>
                <span className="shrink-0 font-mono text-sm text-foreground">{e.label}</span>
                <span className={cn('truncate', negative ? 'text-destructive' : 'text-muted-foreground')}>
                  {e.message}
                </span>
              </li>
            )
          })}
        </ul>
      )}

      {activity.error && <StatusText tone="negative">status API unreachable — {activity.error}</StatusText>}
    </div>
  )
}
