'use client'

import { usePolling } from '@/hooks/use-polling'
import { MonoValue } from '@/components/mono-value'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { AssetAmount, Relays } from '@/lib/types'

async function fetchRelays(): Promise<Relays> {
  const res = await fetch('/api/sidecar/relays', { cache: 'no-store' })
  if (!res.ok) throw new Error(`sidecar /relays returned ${res.status}`)
  return res.json()
}

interface PricedDataRelay {
  label: string
  sidecarUrl: string
  price: AssetAmount | null
  priceError: string | null
}

async function fetchDataRelayPrices(relays: Relays): Promise<PricedDataRelay[]> {
  return Promise.all(
    relays.dataRelays.map(async (relay): Promise<PricedDataRelay> => {
      try {
        const res = await fetch(`/api/relay-price?url=${encodeURIComponent(relay.sidecarUrl)}`, { cache: 'no-store' })
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          throw new Error(body.error ?? `HTTP ${res.status}`)
        }
        return { ...relay, price: await res.json(), priceError: null }
      } catch (err) {
        return {
          ...relay,
          price: null,
          priceError: err instanceof Error ? err.message : String(err),
        }
      }
    })
  )
}

export default function RelaysPage() {
  const relays = usePolling(fetchRelays, 15_000)
  const priced = usePolling(async () => (relays.data ? fetchDataRelayPrices(relays.data) : []), 15_000, [
    relays.data ? JSON.stringify(relays.data.dataRelays) : '',
  ])

  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Relays</h1>
        <p className="text-sm text-muted-foreground">
          Discovered from the tailnet&apos;s relay registry on ENS, not a static manifest — see docs/adr/0003.
        </p>
      </div>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm text-muted-foreground">
          Rendezvous — candidate exchange, metered on every connection attempt
        </h2>
        {!relays.data && !relays.error && <Skeleton className="h-9 w-full" />}
        {relays.data && relays.data.rendezvous.length === 0 && (
          <p className="text-sm text-muted-foreground">None discovered.</p>
        )}
        {relays.data && relays.data.rendezvous.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Label</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Payment sidecar</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {relays.data.rendezvous.map((r) => (
                <TableRow key={r.label}>
                  <TableCell className="font-mono text-sm">{r.label}</TableCell>
                  <TableCell className="font-mono text-sm">{r.address}</TableCell>
                  <TableCell className="font-mono text-sm">{r.sidecarUrl ?? 'unmetered'}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm text-muted-foreground">
          Data relays — bytes-metered fallback, used only when hole punching fails
        </h2>
        {!relays.data && !relays.error && <Skeleton className="h-9 w-full" />}
        {relays.data && relays.data.dataRelays.length === 0 && (
          <p className="text-sm text-muted-foreground">None discovered.</p>
        )}
        {relays.data && relays.data.dataRelays.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Label</TableHead>
                <TableHead>Sidecar</TableHead>
                <TableHead className="text-right">Price / byte (live)</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {relays.data.dataRelays.map((r) => {
                const live = priced.data?.find((p) => p.label === r.label)
                return (
                  <TableRow key={r.label}>
                    <TableCell className="font-mono text-sm">{r.label}</TableCell>
                    <TableCell>
                      <MonoValue value={r.sidecarUrl} display={r.sidecarUrl} href={r.sidecarUrl} />
                    </TableCell>
                    <TableCell className="text-right font-mono text-sm">
                      {live?.price
                        ? `${live.price.amount} atomic ${live.price.asset}`
                        : live?.priceError
                          ? 'unreachable'
                          : '…'}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </section>
    </div>
  )
}
