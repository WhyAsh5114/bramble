'use client'

import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'

export function MonoValue({ value, display, href }: { value: string; display: string; href?: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 1200)
  }

  const text = href ? (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="font-mono text-sm text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
    >
      {display}
    </a>
  ) : (
    <span className="font-mono text-sm text-foreground">{display}</span>
  )

  return (
    <span className="inline-flex items-center gap-1.5">
      {text}
      <button
        type="button"
        onClick={copy}
        aria-label="Copy to clipboard"
        className={cn('text-muted-foreground hover:text-foreground', copied && 'text-foreground')}
      >
        {copied ? <Check size={12} /> : <Copy size={12} />}
      </button>
    </span>
  )
}
