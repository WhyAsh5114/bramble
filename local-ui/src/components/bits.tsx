'use client'

import { useState, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { formatFingerprint } from '../lib/format'

export function Notice({
  tone = 'negative',
  title,
  nextStep,
}: {
  tone?: 'negative' | 'neutral'
  title: string
  nextStep?: string
}) {
  return (
    <div role="alert" className={`notice callout ${tone}`}>
      <strong>{title}</strong>
      {nextStep && <span className="muted">{nextStep}</span>}
    </div>
  )
}

export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="fact">
      <span className="eyebrow">{label}</span>
      <strong className="wrap">{children}</strong>
    </div>
  )
}

export function Fingerprint({ value }: { value: string }) {
  return <code className="fingerprint">{value ? formatFingerprint(value) : 'Created when you continue'}</code>
}

export function CopyValue({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <span className="copy-value">
      <code>{value}</code>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        aria-label={`Copy ${label}`}
        onClick={() => {
          void navigator.clipboard?.writeText(value).then(
            () => {
              setCopied(true)
              setTimeout(() => setCopied(false), 1500)
            },
            () => setCopied(false)
          )
        }}
      >
        {copied ? 'Copied' : 'Copy'}
      </Button>
    </span>
  )
}

export function PageHeading({ eyebrow, title, children }: { eyebrow: string; title: string; children?: ReactNode }) {
  return (
    <div className="page-heading">
      <div>
        <p className="eyebrow">{eyebrow}</p>
        <h1>{title}</h1>
        {children && <p className="muted lead">{children}</p>}
      </div>
    </div>
  )
}
