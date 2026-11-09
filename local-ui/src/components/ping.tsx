'use client'

import { useState } from 'react'
import { Button } from './ui/button'
import { api } from '../lib/api'
import { errorText } from './bits'

/** Pings a peer's built-in ping service through the tunnel and shows the round trip. */
export function PingButton({ label }: { label: string }) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string }>()
  async function ping() {
    setBusy(true)
    try {
      const r = await api.ping(label)
      setResult({ ok: true, text: `${r.millis} ms${r.path ? ` · ${r.path}` : ''}` })
    } catch (e) {
      setResult({ ok: false, text: errorText(e) })
    } finally {
      setBusy(false)
    }
  }
  return (
    <span className="flex items-center gap-2">
      {result && (
        <span
          className={result.ok ? 'mono text-xs text-success-strong' : 'max-w-56 truncate text-xs text-destructive'}
          title={result.text}
          role="status"
        >
          {result.text}
        </span>
      )}
      <Button variant="outline" size="sm" onClick={() => void ping()} disabled={busy}>
        {busy ? 'Pinging…' : 'Ping'}
      </Button>
    </span>
  )
}
