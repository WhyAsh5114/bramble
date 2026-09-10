'use client'

import { useState } from 'react'
import { usePolling } from '@/hooks/use-polling'
import { MonoValue } from '@/components/mono-value'
import { StatusText } from '@/components/status-text'
import { StatusDot } from '@/components/status-dot'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { formatHandshakeAge, isAuthorized, parseExpiry, truncateHex } from '@/lib/format'
import { SEPOLIA_ETHERSCAN_URL } from '@/lib/config'
import type { DeviceRecord, PeerStatus, PingResponse } from '@/lib/types'

interface DeviceRow {
  label: string
  record: DeviceRecord | null
  error: string | null
}

async function fetchDevices(): Promise<DeviceRow[]> {
  const configRes = await fetch('/api/config', { cache: 'no-store' })
  if (!configRes.ok) throw new Error(`config returned ${configRes.status}`)
  const { deviceLabels } = (await configRes.json()) as { deviceLabels: string[] }

  return Promise.all(
    deviceLabels.map(async (label): Promise<DeviceRow> => {
      try {
        const res = await fetch(`/api/sidecar/device/${label}`, {
          cache: 'no-store',
        })
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          throw new Error(body.error ?? `HTTP ${res.status}`)
        }
        return { label, record: await res.json(), error: null }
      } catch (err) {
        return {
          label,
          record: null,
          error: err instanceof Error ? err.message : String(err),
        }
      }
    })
  )
}

// Peers come from this dashboard's own brambled node's status API, not ENS
// — a best-effort live overlay, not authoritative like the device table
// above. Failing to reach it (no local node running, e.g. viewing the
// dashboard purely against ENS state) degrades to an empty map rather than
// breaking the page.
async function fetchPeersByLabel(): Promise<Map<string, PeerStatus>> {
  const res = await fetch('/api/brambled/peers', { cache: 'no-store' })
  if (!res.ok) return new Map()
  const peers = (await res.json()) as PeerStatus[]
  return new Map(peers.map((p) => [p.label, p]))
}

export default function DevicesPage() {
  const devices = usePolling(fetchDevices, 5_000)
  const peers = usePolling(fetchPeersByLabel, 3_000)

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Devices</h1>
        <p className="text-sm text-muted-foreground">
          Authorization is pubkey present, not revoked, and expiry in the future — the same rule brambled&apos;s
          admission loop enforces. Connectivity is this node&apos;s own live WireGuard state, not ENS.
        </p>
      </div>

      {!devices.data && !devices.error && (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-20 w-full rounded-lg" />
          <Skeleton className="h-20 w-full rounded-lg" />
        </div>
      )}

      {devices.data && devices.data.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No devices configured — set <code className="font-mono text-foreground">DEVICE_LABELS</code> in the
          dashboard&apos;s environment (comma-separated labels).
        </p>
      )}

      {devices.data && devices.data.length > 0 && (
        <div className="flex flex-col gap-3">
          {devices.data.map((row) => (
            <DeviceCard key={row.label} row={row} peer={peers.data?.get(row.label) ?? null} />
          ))}
        </div>
      )}

      {devices.error && <p className="text-sm text-destructive">{devices.error}</p>}
    </div>
  )
}

function DeviceCard({ row, peer }: { row: DeviceRow; peer: PeerStatus | null }) {
  if (row.error || !row.record) {
    return (
      <div className="flex items-center gap-3 rounded-lg border border-dashed p-4">
        <StatusDot tone="neutral" />
        <span className="font-mono text-sm text-foreground">{row.label}</span>
        <StatusText tone="negative">unresolved — {row.error}</StatusText>
      </div>
    )
  }

  const { record } = row
  const authorized = isAuthorized(record)
  const expiry = parseExpiry(record.expiry)
  const connected = Boolean(peer?.handshaked)
  const tone = record.revoked ? 'negative' : authorized ? 'positive' : 'neutral'

  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-card p-4 transition-colors hover:border-foreground/20">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <StatusDot tone={tone} pulse={tone === 'positive' && connected} />
          <span className="truncate font-mono text-sm font-medium text-foreground">{row.label}</span>
          {record.revoked ? (
            <StatusText tone="negative">revoked</StatusText>
          ) : authorized ? (
            <StatusText tone="positive">authorized</StatusText>
          ) : (
            <StatusText tone="neutral">not authorized</StatusText>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <ConnectivitySummary peer={peer} />
          <PingButton label={row.label} pingable={connected} />
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5 border-t pt-3 text-xs text-muted-foreground">
        <span className="flex items-center gap-1.5">
          pubkey
          <MonoValue value={record.pubkey ?? ''} display={truncateHex(record.pubkey)} />
        </span>
        <span className={expiry.expired ? 'text-destructive' : undefined}>{expiry.relative}</span>
        <span>
          {record.acl.length} ACL grant{record.acl.length === 1 ? '' : 's'}
        </span>
        <span>
          {record.aclGranters.length} granter{record.aclGranters.length === 1 ? '' : 's'}
        </span>
        <span className="flex items-center gap-1.5">
          token
          <MonoValue
            value={record.tokenId}
            display={truncateHex(record.tokenId, 4, 4)}
            href={`${SEPOLIA_ETHERSCAN_URL}/search?q=${record.tokenId}`}
          />
        </span>
        {record.resolverAddress && (
          <span className="flex items-center gap-1.5">
            own resolver
            <MonoValue
              value={record.resolverAddress}
              display={truncateHex(record.resolverAddress, 4, 4)}
              href={`${SEPOLIA_ETHERSCAN_URL}/address/${record.resolverAddress}`}
            />
          </span>
        )}
      </div>
    </div>
  )
}

function ConnectivitySummary({ peer }: { peer: PeerStatus | null }) {
  if (!peer) return <StatusText tone="neutral">not tracked here</StatusText>
  if (!peer.handshaked) return <StatusText tone="neutral">no handshake yet</StatusText>
  return <StatusText tone="positive">handshake {formatHandshakeAge(peer.lastHandshakeUnixNs)}</StatusText>
}

function PingButton({ label, pingable }: { label: string; pingable: boolean }) {
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<PingResponse | null>(null)

  async function ping() {
    setPending(true)
    setResult(null)
    try {
      const res = await fetch('/api/brambled/ping', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label }),
      })
      const body = (await res.json()) as PingResponse & { error?: string }
      setResult(res.ok ? body : { reachable: false, detail: body.error ?? `HTTP ${res.status}` })
    } catch (err) {
      setResult({ reachable: false, detail: err instanceof Error ? err.message : String(err) })
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex items-center gap-2">
      {result && (
        <span className={result.reachable ? 'text-xs text-positive' : 'text-xs text-destructive'}>
          {result.reachable ? (result.rtt ?? 'reply') : (result.detail ?? 'no reply')}
        </span>
      )}
      <Button size="sm" variant={pingable ? 'default' : 'outline'} disabled={!pingable || pending} onClick={ping}>
        {pending ? 'Pinging…' : 'Ping'}
      </Button>
    </div>
  )
}
