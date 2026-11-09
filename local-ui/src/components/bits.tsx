'use client'

import Link from 'next/link'
import { useState, type ReactNode } from 'react'
import { Check, CircleAlert, Copy, ExternalLink, Loader2 } from 'lucide-react'
import { cn } from 'cn'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Card } from './ui/card'
import type { TxStep } from '../lib/wallet'
import { useApp } from './app-state'

export function PageHeader({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children?: ReactNode
}) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="m-0 text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
      </div>
      {children}
    </div>
  )
}

export function Section({
  id,
  title,
  hint,
  children,
}: {
  id: string
  title: string
  hint?: ReactNode
  children: ReactNode
}) {
  return (
    <section aria-labelledby={id} className="mb-8">
      <div className="mb-3 flex items-baseline justify-between gap-3">
        <h2 id={id}>{title}</h2>
        {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </section>
  )
}

export function Stats({ items }: { items: [string, ReactNode][] }) {
  return (
    <section className="mb-8 grid grid-cols-2 gap-2 sm:gap-4 lg:grid-cols-4" aria-label="Summary">
      {items.map(([label, value]) => (
        <Card key={label} className="min-w-0 gap-1 px-3 sm:gap-2 sm:px-5">
          <span className="eyebrow !text-[10px] sm:!text-xs">{label}</span>
          <strong className="truncate text-xl font-semibold tabular-nums sm:text-2xl">{value}</strong>
        </Card>
      ))}
    </section>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="empty m-0 text-sm">{children}</p>
}

export function Problem({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="notice negative m-0 mb-4 flex items-start gap-2 text-sm">
      <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <span className="min-w-0 break-words">{children}</span>
    </p>
  )
}

export function CopyButton({ value, label = 'Copy' }: { value: string; label?: string }) {
  const [done, setDone] = useState(false)
  return (
    <Button
      variant="outline"
      size="sm"
      onClick={() => {
        void navigator.clipboard?.writeText(value)
        setDone(true)
        setTimeout(() => setDone(false), 1500)
      }}
    >
      {done ? <Check /> : <Copy />}
      {done ? 'Copied' : label}
    </Button>
  )
}

export function Dot({ tone }: { tone: 'ok' | 'warn' | 'bad' | 'off' }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'inline-block size-2 shrink-0 rounded-full',
        tone === 'ok' && 'bg-success',
        tone === 'warn' && 'bg-warning',
        tone === 'bad' && 'bg-destructive',
        tone === 'off' && 'bg-muted-foreground/50'
      )}
    />
  )
}

export function PathBadge({ connected, path }: { connected: boolean; path?: string }) {
  if (!connected) return <Badge variant="outline">Not connected</Badge>
  if (path === 'direct') return <Badge variant="success">Direct</Badge>
  if (path === 'relay' || path === 'relayed') return <Badge variant="warning">Relayed</Badge>
  return <Badge variant="outline">Connected</Badge>
}

/** A link to a transaction on the block explorer. */
export function TxLink({ hash }: { hash: string }) {
  const { state } = useApp()
  const base = state?.network.explorerTx ?? 'https://sepolia.etherscan.io/tx/'
  return (
    <a className="mono inline-flex items-center gap-1 text-xs" href={`${base}${hash}`} target="_blank" rel="noreferrer">
      {hash.slice(0, 10)}…
      <ExternalLink className="size-3" aria-hidden="true" />
    </a>
  )
}

export function TxSteps({ steps }: { steps: TxStep[] }) {
  if (!steps.length) return null
  return (
    <ol className="m-0 mt-4 grid list-none gap-2 p-0" aria-label="Transaction progress">
      {steps.map((s) => (
        <li key={s.label} className="flex items-start gap-3 text-sm">
          <span className="mt-0.5 grid size-4 place-items-center">
            {s.status === 'done' && <Check className="size-4 text-success" aria-label="done" />}
            {s.status === 'failed' && <CircleAlert className="size-4 text-destructive" aria-label="failed" />}
            {(s.status === 'signing' || s.status === 'pending') && (
              <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="in progress" />
            )}
            {s.status === 'waiting' && <Dot tone="off" />}
          </span>
          <span className="min-w-0 flex-1">
            <span className={cn(s.status === 'waiting' && 'text-muted-foreground')}>{s.label}</span>
            {s.status === 'signing' && (
              <span className="ml-2 text-xs text-muted-foreground">confirm in your wallet, then wait for a block</span>
            )}
            {s.error && <span className="block break-words text-xs text-destructive">{s.error}</span>}
          </span>
          {s.hash && <TxLink hash={s.hash} />}
        </li>
      ))}
    </ol>
  )
}

/** Shown on tailnet pages before this device has chosen a tailnet. */
export function NeedsTailnet({ children }: { children: ReactNode }) {
  const { state } = useApp()
  if (!state) return <Empty>Loading…</Empty>
  if (!state.tailnet)
    return (
      <Empty>
        This device is not on a tailnet yet. <Link href="/">Create or join one</Link> first.
      </Empty>
    )
  return <>{children}</>
}

export function errorText(e: unknown) {
  if (e instanceof Error) {
    const m = e.message.split('\n')[0]
    return m.includes('User rejected') || m.includes('User denied') ? 'You declined the request in your wallet.' : m
  }
  return String(e)
}
