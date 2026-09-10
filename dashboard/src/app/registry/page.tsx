'use client'

import { usePolling } from '@/hooks/use-polling'
import { MonoValue } from '@/components/mono-value'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusText } from '@/components/status-text'
import { SEPOLIA_ETHERSCAN_URL } from '@/lib/config'
import type { DeviceRecord, SidecarHealth } from '@/lib/types'

async function fetchHealth(): Promise<SidecarHealth> {
  const res = await fetch('/api/sidecar/health', { cache: 'no-store' })
  if (!res.ok) throw new Error(`sidecar /health returned ${res.status}`)
  return res.json()
}

interface ResolverRow {
  label: string
  resolverAddress: string | null
  error: string | null
}

async function fetchResolvers(): Promise<ResolverRow[]> {
  const configRes = await fetch('/api/config', { cache: 'no-store' })
  if (!configRes.ok) throw new Error(`config returned ${configRes.status}`)
  const { deviceLabels } = (await configRes.json()) as { deviceLabels: string[] }

  return Promise.all(
    deviceLabels.map(async (label): Promise<ResolverRow> => {
      try {
        const res = await fetch(`/api/sidecar/device/${label}`, { cache: 'no-store' })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const record = (await res.json()) as DeviceRecord
        return { label, resolverAddress: record.resolverAddress, error: null }
      } catch (err) {
        return { label, resolverAddress: null, error: err instanceof Error ? err.message : String(err) }
      }
    })
  )
}

export default function RegistryPage() {
  const health = usePolling(fetchHealth, 30_000)
  const resolvers = usePolling(fetchResolvers, 30_000)

  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Registry</h1>
        <p className="text-sm text-muted-foreground">
          Two structurally separate ENSv2 Permissioned Registries, not one shared table — device identity and relay
          endpoints can never end up in the same contract, even by mistake (docs/adr/0003). Every device also gets its
          own dedicated Permissioned Resolver, deployed fresh at enrollment, never shared with another device.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-2 rounded-lg border bg-card p-4">
          <span className="text-xs text-muted-foreground">Tailnet registry — device identity</span>
          {health.data ? (
            <MonoValue
              value={health.data.tailnetRegistry}
              display={health.data.tailnetRegistry}
              href={`${SEPOLIA_ETHERSCAN_URL}/address/${health.data.tailnetRegistry}`}
            />
          ) : (
            <Skeleton className="h-5 w-64" />
          )}
          <span className="text-xs text-muted-foreground">
            pubkey, revoked, acl, acl-granters — never an endpoint (host/IP/port), enforced by{' '}
            <code className="font-mono">scripts/check-no-endpoints.mjs</code>
          </span>
        </div>

        <div className="flex flex-col gap-2 rounded-lg border bg-card p-4">
          <span className="text-xs text-muted-foreground">Relay registry — rendezvous &amp; data-relay endpoints</span>
          {health.data ? (
            health.data.relayRegistry ? (
              <MonoValue
                value={health.data.relayRegistry}
                display={health.data.relayRegistry}
                href={`${SEPOLIA_ETHERSCAN_URL}/address/${health.data.relayRegistry}`}
              />
            ) : (
              <StatusText tone="neutral">not configured on this sidecar</StatusText>
            )
          ) : (
            <Skeleton className="h-5 w-64" />
          )}
          <span className="text-xs text-muted-foreground">
            rendezvous-address, sidecar-url, price-per-byte — the only records this project ever publishes endpoint data
            in
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <h2 className="text-sm text-muted-foreground">Per-device resolvers</h2>
        {!resolvers.data && !resolvers.error && (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-10 w-full rounded-lg" />
            <Skeleton className="h-10 w-full rounded-lg" />
          </div>
        )}
        {resolvers.data && resolvers.data.length > 0 && (
          <ul className="flex flex-col gap-2">
            {resolvers.data.map((r) => (
              <li
                key={r.label}
                className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-lg border bg-card px-4 py-3"
              >
                <span className="font-mono text-sm text-foreground">{r.label}</span>
                {r.error || !r.resolverAddress ? (
                  <StatusText tone="negative">{r.error ?? 'no resolver set'}</StatusText>
                ) : (
                  <MonoValue
                    value={r.resolverAddress}
                    display={r.resolverAddress}
                    href={`${SEPOLIA_ETHERSCAN_URL}/address/${r.resolverAddress}`}
                  />
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
