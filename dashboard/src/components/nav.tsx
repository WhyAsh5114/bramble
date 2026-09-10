'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { cn } from '@/lib/utils'

const links = [
  { href: '/', label: 'Overview' },
  { href: '/devices', label: 'Devices' },
  { href: '/relays', label: 'Relays' },
  { href: '/activity', label: 'Activity' },
]

export function Nav() {
  const pathname = usePathname()
  return (
    <header className="border-b">
      <div className="mx-auto flex h-14 max-w-5xl items-center gap-8 px-6">
        <span className="font-mono text-sm text-foreground">bramble</span>
        <nav className="flex items-center gap-6">
          {links.map((link) => {
            const active = pathname === link.href
            return (
              <Link
                key={link.href}
                href={link.href}
                className={cn(
                  'text-sm transition-colors',
                  active ? 'text-foreground' : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {link.label}
              </Link>
            )
          })}
        </nav>
      </div>
    </header>
  )
}
