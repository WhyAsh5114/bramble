'use client'

import { useEffect, useRef, useState } from 'react'

export interface PollState<T> {
  data: T | null
  error: string | null
  loading: boolean
  lastUpdated: Date | null
}

// Polls fetcher every intervalMs, keeping the last good value on screen
// through a failed tick instead of flashing an error state — a single
// dropped resolve (sidecar restart, RPC hiccup) shouldn't make the dashboard
// look broken mid-demo. deps re-triggers the effect (e.g. once device labels
// load from /api/config).
export function usePolling<T>(fetcher: () => Promise<T>, intervalMs: number, deps: unknown[] = []): PollState<T> {
  const [state, setState] = useState<PollState<T>>({
    data: null,
    error: null,
    loading: true,
    lastUpdated: null,
  })
  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher

  useEffect(() => {
    let cancelled = false

    async function tick() {
      try {
        const data = await fetcherRef.current()
        if (cancelled) return
        setState({ data, error: null, loading: false, lastUpdated: new Date() })
      } catch (err) {
        if (cancelled) return
        setState((prev) => ({
          ...prev,
          error: err instanceof Error ? err.message : String(err),
          loading: false,
        }))
      }
    }

    tick()
    const id = setInterval(tick, intervalMs)
    return () => {
      cancelled = true
      clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return state
}
