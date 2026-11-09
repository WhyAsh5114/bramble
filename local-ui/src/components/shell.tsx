'use client'

import Image from 'next/image'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useEffect, useState, type ReactNode } from 'react'
import {
  Activity,
  Boxes,
  Gauge,
  KeyRound,
  Laptop,
  Menu,
  Moon,
  Network,
  RadioTower,
  Settings,
  ShieldCheck,
  Sun,
  Wallet,
  X,
} from 'lucide-react'
import { formatEther } from 'viem'
import { cn } from 'cn'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { useApp } from './app-state'
import { useWallet } from '../lib/wallet'
import { shortAddress, type Phase } from '../lib/api'

type NavItem = { href: string; label: string; icon: typeof Laptop }

const groups: { name: string; items: NavItem[] }[] = [
  {
    name: 'This device',
    items: [
      { href: '/', label: 'Overview', icon: Gauge },
      { href: '/services', label: 'Services', icon: Boxes },
      { href: '/settings', label: 'Settings', icon: Settings },
    ],
  },
  {
    name: 'Tailnet',
    items: [
      { href: '/devices', label: 'Devices', icon: Laptop },
      { href: '/access', label: 'Access', icon: ShieldCheck },
      { href: '/network', label: 'Network', icon: Network },
      { href: '/relays', label: 'Relays', icon: RadioTower },
      { href: '/activity', label: 'Activity', icon: Activity },
    ],
  },
]

const titles: Record<string, string> = Object.fromEntries(groups.flatMap((g) => g.items.map((i) => [i.href, i.label])))

const phaseText: Record<Phase, [string, 'success' | 'warning' | 'destructive' | 'outline']> = {
  setup: ['Not on a tailnet', 'outline'],
  syncing: ['Syncing chain', 'warning'],
  waiting: ['Waiting for admin', 'warning'],
  member: ['Member', 'success'],
  revoked: ['Revoked', 'destructive'],
}

function WalletPanel() {
  const w = useWallet()
  const { state } = useApp()
  if (!w.available)
    return (
      <div className="rounded-lg border border-divider bg-secondary/50 p-3 text-xs text-muted-foreground">
        <div className="mb-1 flex items-center gap-2 text-sm font-medium text-foreground">
          <Wallet className="size-4" aria-hidden="true" /> No wallet
        </div>
        Members need no wallet. To create or run a tailnet, open this page in a browser with a wallet extension.
      </div>
    )
  if (!w.address)
    return (
      <div className="rounded-lg border border-divider bg-secondary/50 p-3">
        <div className="flex items-center gap-2 text-sm font-medium">
          <Wallet className="size-4 text-muted-foreground" aria-hidden="true" />
          Admin wallet
        </div>
        <p className="mt-1 text-xs text-muted-foreground">Admins sign changes with their own wallet on Sepolia.</p>
        <Button variant="outline" className="mt-3 w-full" onClick={() => void w.connect()} disabled={w.connecting}>
          {w.connecting ? 'Connecting…' : 'Connect wallet'}
        </Button>
        {w.error && <p className="mt-2 text-xs text-destructive">{w.error}</p>}
      </div>
    )
  return (
    <div className="rounded-lg border border-divider bg-secondary/50 p-3" role="group" aria-label="Wallet">
      <div className="flex items-center gap-2 text-sm font-medium">
        <KeyRound className={cn('size-4', w.admin ? 'text-success' : 'text-muted-foreground')} aria-hidden="true" />
        <span className="mono">{shortAddress(w.address)}</span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        {!w.chainOk
          ? 'Wrong network.'
          : state?.tailnet
            ? w.admin
              ? 'Admin of this tailnet.'
              : 'Not an admin of this tailnet.'
            : 'Ready to create a tailnet.'}
        {w.chainOk && w.balance !== undefined && ` ${Number(formatEther(w.balance)).toFixed(4)} SepoliaETH.`}
      </p>
      {!w.chainOk ? (
        <Button variant="outline" className="mt-3 w-full" onClick={() => void w.connect()}>
          Switch to Sepolia
        </Button>
      ) : (
        <Button variant="ghost" size="sm" className="mt-2 w-full" onClick={w.disconnect}>
          Disconnect
        </Button>
      )}
    </div>
  )
}

function SidebarNav({ onNavigate }: { onNavigate: () => void }) {
  const pathname = usePathname()
  const { state, devices } = useApp()
  const online = devices?.filter((d) => d.connected && !d.self).length
  return (
    <nav aria-label="Console" className="flex-1 overflow-y-auto px-3 py-4">
      {groups.map((group) => (
        <div key={group.name} className="mb-5">
          <p className="eyebrow px-3 pb-2">{group.name}</p>
          <ul className="m-0 grid list-none gap-0.5 p-0">
            {group.items.map(({ href, label, icon: Icon }) => {
              const active = href === '/' ? pathname === '/' : pathname.startsWith(href)
              const dim = group.name === 'Tailnet' && !state?.tailnet
              return (
                <li key={href}>
                  <Link
                    href={href}
                    onClick={onNavigate}
                    aria-current={active ? 'page' : undefined}
                    className={cn(
                      'flex items-center gap-3 rounded-md px-3 py-2 text-sm no-underline outline-offset-2 hover:bg-secondary focus-visible:outline-2 focus-visible:outline-ring',
                      active
                        ? 'bg-secondary font-medium text-foreground'
                        : 'text-muted-foreground hover:text-foreground',
                      dim && 'opacity-60'
                    )}
                  >
                    <Icon className="size-4 shrink-0" aria-hidden="true" />
                    <span className="flex-1">{label}</span>
                    {href === '/devices' && !!online && (
                      <Badge variant="success" className="px-2 py-0 tabular-nums" title="Peers connected">
                        {online}
                      </Badge>
                    )}
                  </Link>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}

function useTheme() {
  const [theme, setTheme] = useState<'light' | 'dark'>('light')
  useEffect(() => {
    let saved: string | null = null
    try {
      saved = localStorage.getItem('bramble-theme')
    } catch {}
    const t =
      saved === 'dark' || saved === 'light'
        ? saved
        : matchMedia('(prefers-color-scheme: dark)').matches
          ? 'dark'
          : 'light'
    setTheme(t)
  }, [])
  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])
  return [
    theme,
    (t: 'light' | 'dark') => {
      setTheme(t)
      try {
        localStorage.setItem('bramble-theme', t)
      } catch {}
    },
  ] as const
}

export function Shell({ children }: { children: ReactNode }) {
  const pathname = usePathname()
  const { state, error, updatedAt } = useApp()
  const [open, setOpen] = useState(false)
  const [theme, setTheme] = useTheme()
  const title = titles[pathname.replace(/\/$/, '') || '/'] ?? 'Overview'
  const phase = state ? phaseText[state.phase] : undefined
  return (
    <div className="flex h-dvh overflow-hidden bg-background">
      <a className="skip-link" href="#main-content">
        Skip to main content
      </a>
      {open && (
        <div className="fixed inset-0 z-20 bg-black/40 md:hidden" onClick={() => setOpen(false)} aria-hidden="true" />
      )}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-30 flex w-64 flex-col border-r border-divider bg-card transition-transform md:static md:translate-x-0',
          open ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <div className="flex h-14 shrink-0 items-center justify-between border-b border-divider px-4">
          <Link href="/" className="flex items-center gap-2 font-semibold no-underline" onClick={() => setOpen(false)}>
            <Image src="/bramble-logo.png" alt="" width={26} height={26} unoptimized />
            bramble
          </Link>
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            aria-label="Close menu"
            onClick={() => setOpen(false)}
          >
            <X />
          </Button>
        </div>
        <div className="border-b border-divider px-4 py-3">
          <p className="eyebrow">Tailnet</p>
          <p className="mono mt-1 truncate text-sm" title={state?.tailnet?.name}>
            {state?.tailnet?.name ?? <span className="text-muted-foreground">none yet</span>}
          </p>
          {state?.label && (
            <p className="mt-0.5 truncate text-xs text-muted-foreground">
              this device: <span className="mono">{state.label}</span>
            </p>
          )}
        </div>
        <SidebarNav onNavigate={() => setOpen(false)} />
        <div className="shrink-0 border-t border-divider p-3">
          <WalletPanel />
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b border-divider bg-card px-4 md:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            aria-label="Open menu"
            onClick={() => setOpen(true)}
          >
            <Menu />
          </Button>
          <span className="text-sm font-medium">{title}</span>
          <div className="ml-auto flex items-center gap-2">
            {phase && (
              <Badge variant={phase[1]} className="hidden sm:inline-flex">
                {phase[0]}
              </Badge>
            )}
            <Badge variant={error ? 'destructive' : 'outline'}>
              <span
                className={cn(
                  'size-1.5 rounded-full',
                  error ? 'bg-destructive' : updatedAt ? 'bg-success' : 'bg-muted-foreground'
                )}
                aria-hidden="true"
              />
              {error ? 'Daemon offline' : updatedAt ? 'Live' : 'Connecting…'}
            </Badge>
            <Badge variant="outline" className="mono hidden sm:inline-flex">
              Sepolia
            </Badge>
            <Button
              variant="ghost"
              size="icon"
              aria-label={theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'}
              onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}
            >
              {theme === 'light' ? <Moon /> : <Sun />}
            </Button>
          </div>
        </header>
        <main id="main-content" className="m-0 max-w-none min-h-0 flex-1 overflow-y-auto p-0">
          <div className="mx-auto w-full max-w-6xl p-4 md:p-8">
            {error && !state && (
              <p role="alert" className="notice negative mb-6 text-sm">
                Can&apos;t reach the bramble daemon on this machine. Start it with <code>bramble up</code>, then reload.
              </p>
            )}
            {children}
          </div>
          <footer className="mx-auto flex w-full max-w-6xl flex-wrap justify-between gap-2 px-4 pb-8 text-xs text-muted-foreground md:px-8">
            <span>
              bramble {state?.version ?? ''} · membership lives on Sepolia; this page talks only to the daemon on this
              machine and your wallet.
            </span>
            {updatedAt && <span>Updated {updatedAt.toLocaleTimeString()}</span>}
          </footer>
        </main>
      </div>
    </div>
  )
}
