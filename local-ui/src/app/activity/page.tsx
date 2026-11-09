'use client'

import { useEffect, useState } from 'react'
import { Badge } from '../../components/ui/badge'
import { Card } from '../../components/ui/card'
import { Empty, NeedsTailnet, PageHeader } from '../../components/bits'
import { api, type ActivityEvent } from '../../lib/api'

function tone(e: ActivityEvent): 'success' | 'warning' | 'destructive' | 'outline' {
  const o = (e.outcome ?? '').toLowerCase()
  if (/(ok|allow|admit|connect|success|up)/.test(o)) return 'success'
  if (/(deny|reject|revok|fail|error)/.test(o)) return 'destructive'
  if (o) return 'warning'
  return 'outline'
}

export default function ActivityPage() {
  const [events, setEvents] = useState<ActivityEvent[]>()
  useEffect(() => {
    let stop = false
    const load = () =>
      api
        .activity()
        .then((e) => !stop && setEvents([...e].reverse()))
        .catch(() => !stop && setEvents((x) => x ?? []))
    void load()
    const t = setInterval(load, 3000)
    return () => {
      stop = true
      clearInterval(t)
    }
  }, [])
  return (
    <>
      <PageHeader title="Activity" description="What this device's node saw: admissions, paths and service requests." />
      <NeedsTailnet>
        {!events ? (
          <Empty>Loading…</Empty>
        ) : events.length === 0 ? (
          <Empty>Nothing yet. Events appear as peers connect and use services.</Empty>
        ) : (
          <Card className="gap-0 overflow-x-auto py-0">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Event</th>
                  <th>Peer</th>
                  <th>Detail</th>
                </tr>
              </thead>
              <tbody>
                {events.map((e, i) => (
                  <tr key={`${e.time}-${i}`}>
                    <td className="mono text-xs whitespace-nowrap text-muted-foreground">
                      {new Date(e.time).toLocaleTimeString()}
                    </td>
                    <td>
                      <Badge variant={tone(e)}>{e.outcome ? `${e.kind} · ${e.outcome}` : e.kind}</Badge>
                    </td>
                    <td className="mono">{e.label ?? '—'}</td>
                    <td className="text-sm">{e.message}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        )}
      </NeedsTailnet>
    </>
  )
}
