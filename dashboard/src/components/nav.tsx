'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { cn } from '@/lib/utils'

const links = [
  { href: '/', label: 'Overview' },
  { href: '/devices', label: 'Devices' },
  { href: '/relays', label: 'Relays' },
  { href: '/payments', label: 'Payments' },
  { href: '/registry', label: 'Registry' },
  { href: '/activity', label: 'Activity' },
]

export function Nav() {
  const pathname = usePathname()
  return (
    <header className="sticky top-0 z-10 border-b bg-background/80 backdrop-blur-sm">
      <div className="mx-auto flex h-14 max-w-6xl items-center gap-8 px-6">
        <Link href="/" className="flex items-center gap-2">
          <span className="size-2.5 rounded-full bg-primary" />
          <span className="font-mono text-sm font-medium text-foreground">bramble</span>
        </Link>
        <nav className="flex h-full items-center gap-1">
          {links.map((link) => {
            const active = pathname === link.href
            return (
              <Link
                key={link.href}
                href={link.href}
                className={cn(
                  'relative flex h-full items-center px-3 text-sm transition-colors',
                  active ? 'text-foreground' : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {link.label}
                {active && <span className="absolute inset-x-3 bottom-0 h-0.5 rounded-full bg-primary" />}
              </Link>
            )
          })}
        </nav>
      </div>
    </header>
  )
}
