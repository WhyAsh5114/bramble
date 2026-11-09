'use client'

import { memo, useEffect, useMemo, useState } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  Panel,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import { Laptop, Waypoints } from 'lucide-react'
import { cn } from 'cn'
import { Card } from '../../components/ui/card'
import { useApp } from '../../components/app-state'
import { NeedsTailnet, PageHeader, PathBadge } from '../../components/bits'
import { PingButton } from '../../components/ping'
import { api, type Device, type RelayInfo } from '../../lib/api'

type Item = { kind: 'device'; id: string; device: Device } | { kind: 'relay'; id: string; relay: RelayInfo }
type FlowNode = Node<{ item: Item; selected: boolean }, 'peer'>

const NODE_W = 184
const NODE_H = 60

function tone(item: Item) {
  if (item.kind === 'relay') return item.relay.error ? 'bg-destructive' : 'bg-success'
  const d = item.device
  if (d.revoked || d.expired) return 'bg-destructive'
  if (d.self || d.connected) return 'bg-success'
  return 'bg-muted-foreground'
}

function subtitle(item: Item) {
  if (item.kind === 'relay')
    return item.relay.error
      ? 'Relay · unreachable'
      : `Relay${item.relay.millis !== undefined ? ` · ${item.relay.millis} ms` : ''}`
  const d = item.device
  if (d.self) return `This device · ${d.meshIp?.split('/')[0] ?? ''}`
  if (d.revoked) return 'Revoked'
  if (!d.pubkey) return 'Not enrolled'
  if (!d.connected) return 'No observed link'
  return d.path === 'direct' ? 'Direct' : d.path?.startsWith('relay') ? 'Relayed' : 'Connected'
}

const PeerNode = memo(function PeerNode({ data }: NodeProps<FlowNode>) {
  const { item, selected } = data
  const Icon = item.kind === 'relay' ? Waypoints : Laptop
  const title = item.kind === 'relay' ? item.relay.label : item.device.label
  const sub = subtitle(item)
  const hidden = { top: '50%', left: '50%', opacity: 0, pointerEvents: 'none' } as const
  return (
    <div
      style={{ width: NODE_W, height: NODE_H }}
      className={cn(
        'flex items-center gap-3 rounded-xl border bg-card px-3 shadow-sm transition-shadow',
        selected ? 'border-foreground ring-2 ring-foreground/15' : 'border-divider hover:shadow-md',
        item.kind === 'device' && item.device.revoked && 'border-dashed border-destructive/60'
      )}
    >
      <Handle type="target" position={Position.Top} style={hidden} isConnectable={false} />
      <Handle type="source" position={Position.Top} style={hidden} isConnectable={false} />
      <span className="relative grid size-9 shrink-0 place-items-center rounded-lg bg-secondary text-foreground">
        <Icon className="size-4" aria-hidden="true" />
        <span
          className={cn('absolute -top-0.5 -right-0.5 size-2.5 rounded-full border-2 border-card', tone(item))}
          aria-hidden="true"
        />
      </span>
      <span className="min-w-0 leading-tight">
        <span className="mono block truncate text-[13px] font-semibold" title={title}>
          {title}
        </span>
        <span className="block truncate text-[11px] text-muted-foreground" title={sub}>
          {sub}
        </span>
      </span>
    </div>
  )
})

const nodeTypes = { peer: PeerNode }

function edgeStyle(path?: string) {
  if (path === 'direct') return { stroke: 'var(--foreground)', strokeWidth: 2 }
  if (path?.startsWith('relay')) return { stroke: 'var(--muted-foreground)', strokeWidth: 2, strokeDasharray: '7 5' }
  return {
    stroke: 'var(--muted-foreground)',
    strokeWidth: 1.5,
    strokeDasharray: '2 6',
    strokeLinecap: 'round' as const,
  }
}

function Graph({
  items,
  selected,
  onSelect,
}: {
  items: Item[]
  selected: string | null
  onSelect: (id: string) => void
}) {
  const nodes = useMemo<FlowNode[]>(() => {
    const relays = items.filter((i) => i.kind === 'relay')
    const self = items.find((i) => i.kind === 'device' && i.device.self)
    const others = items.filter((i) => i.kind === 'device' && !i.device.self)
    const pos = new Map<string, { x: number; y: number }>()
    const GAP = NODE_W + 36
    const row = (list: Item[], y: number) =>
      list.forEach((n, i) => pos.set(n.id, { x: (i - (list.length - 1) / 2) * GAP, y }))
    let y = 0
    if (relays.length) {
      row(relays, y)
      y += 150
    }
    if (self) {
      pos.set(self.id, { x: 0, y })
      y += 170
    }
    for (let i = 0; i < others.length; i += 4) {
      row(others.slice(i, i + 4), y)
      y += 110
    }
    return items.map((item) => {
      const p = pos.get(item.id) ?? { x: 0, y: 0 }
      return {
        id: item.id,
        type: 'peer',
        position: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 },
        data: { item, selected: item.id === selected },
        draggable: false,
      }
    })
  }, [items, selected])
  const edges = useMemo<Edge[]>(() => {
    const self = items.find((i) => i.kind === 'device' && i.device.self)
    if (!self) return []
    const out: Edge[] = []
    for (const i of items) {
      if (i.kind !== 'device' || i.device.self || i.device.revoked || !i.device.pubkey) continue
      const path = i.device.connected ? i.device.path : undefined
      out.push({
        id: `${self.id}:${i.id}`,
        source: self.id,
        target: i.id,
        type: 'straight',
        animated: !!path?.startsWith('relay'),
        style: edgeStyle(path),
        selectable: false,
        focusable: false,
      })
    }
    for (const r of items) {
      if (r.kind !== 'relay' || r.relay.error) continue
      out.push({
        id: `${r.id}:${self.id}`,
        source: r.id,
        target: self.id,
        type: 'straight',
        style: { stroke: 'var(--divider)', strokeWidth: 1.5 },
        selectable: false,
        focusable: false,
      })
    }
    return out
  }, [items])
  return (
    <ReactFlow
      key={items.map((i) => i.id).join('|')}
      className="bramble-flow"
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      fitView
      fitViewOptions={{ padding: 0.18, maxZoom: 1.1 }}
      minZoom={0.3}
      maxZoom={1.6}
      nodesDraggable={false}
      nodesConnectable={false}
      edgesFocusable={false}
      onNodeClick={(_, n) => onSelect(n.id)}
      proOptions={{ hideAttribution: true }}
      aria-label="Tailnet connections"
    >
      <Background variant={BackgroundVariant.Dots} gap={20} size={1.2} />
      <Controls showInteractive={false} position="top-right" />
      <Panel position="bottom-left" className="!m-3">
        <ul className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 rounded-lg border border-divider bg-card/95 px-3 py-2 text-xs text-muted-foreground shadow-sm">
          {(
            [
              ['Direct', 'solid'],
              ['Relayed', 'dashed'],
              ['No link', 'dotted'],
            ] as const
          ).map(([label, style]) => (
            <li key={label} className="flex items-center gap-2">
              <span
                className="inline-block w-6 border-t-2 border-foreground"
                style={{ borderTopStyle: style, opacity: style === 'solid' ? 1 : 0.6 }}
                aria-hidden="true"
              />
              {label}
            </li>
          ))}
        </ul>
      </Panel>
    </ReactFlow>
  )
}

function Details({ item }: { item?: Item }) {
  if (!item) return <p className="m-0 text-sm text-muted-foreground">Select a node to see its details.</p>
  if (item.kind === 'relay') {
    const r = item.relay
    return (
      <dl className="m-0 grid gap-3 text-sm">
        <Row k="Relay" v={<span className="mono">{r.name}</span>} />
        <Row k="Rendezvous" v={<span className="mono">{r.address}</span>} />
        <Row k="Data relay" v={r.dataRelay ? 'Yes' : 'No'} />
        {r.region && <Row k="Region" v={r.region} />}
        <Row
          k="Round trip"
          v={r.error ? <span className="text-destructive">{r.error}</span> : `${r.millis ?? '—'} ms`}
        />
      </dl>
    )
  }
  const d = item.device
  return (
    <dl className="m-0 grid gap-3 text-sm">
      <Row k="Device" v={<span className="mono">{d.name}</span>} />
      <Row k="Mesh address" v={<span className="mono">{d.meshIp ?? '—'}</span>} />
      <Row k="Fingerprint" v={<span className="mono">{d.fingerprint ?? '—'}</span>} />
      {!d.self && <Row k="Path" v={<PathBadge connected={d.connected} path={d.path} />} />}
      {d.endpoint && <Row k="Endpoint" v={<span className="mono">{d.endpoint}</span>} />}
      {!d.self && d.pubkey && !d.revoked && (
        <div>
          <PingButton label={d.label} />
        </div>
      )}
    </dl>
  )
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div>
      <dt className="eyebrow">{k}</dt>
      <dd className="m-0 mt-0.5 break-all">{v}</dd>
    </div>
  )
}

export default function NetworkPage() {
  const { devices } = useApp()
  const [relays, setRelays] = useState<RelayInfo[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  useEffect(() => {
    api
      .relays()
      .then((r) => setRelays(r.filter((x) => !x.error).slice(0, 3)))
      .catch(() => {})
  }, [])
  const items = useMemo<Item[]>(
    () => [
      ...relays.map((r) => ({ kind: 'relay' as const, id: `relay:${r.label}`, relay: r })),
      ...(devices ?? []).map((d) => ({ kind: 'device' as const, id: `device:${d.label}`, device: d })),
    ],
    [relays, devices]
  )
  return (
    <>
      <PageHeader title="Network" description="Links as this device sees them right now." />
      <NeedsTailnet>
        <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_300px]">
          <Card className="h-[560px] overflow-hidden py-0">
            <Graph items={items} selected={selected} onSelect={setSelected} />
          </Card>
          <Card className="gap-3 px-5">
            <Details item={items.find((i) => i.id === selected)} />
          </Card>
        </div>
      </NeedsTailnet>
    </>
  )
}
