import { cn } from '@/lib/utils'

// Color is always paired with StatusText's own wording alongside it — the
// dot is a fast-scan cue for a card grid, not the only signal (same
// discipline status-text.tsx already documents, just with a visual anchor
// added since a wall of gray table text was the actual complaint this
// component exists to fix).
export function StatusDot({ tone, pulse = false }: { tone: 'positive' | 'negative' | 'neutral'; pulse?: boolean }) {
  return (
    <span className="relative inline-flex size-2.5 shrink-0">
      {pulse && (
        <span
          className={cn(
            'absolute inline-flex h-full w-full animate-ping rounded-full opacity-60',
            tone === 'positive' && 'bg-positive',
            tone === 'negative' && 'bg-destructive',
            tone === 'neutral' && 'bg-muted-foreground'
          )}
        />
      )}
      <span
        className={cn(
          'relative inline-flex size-2.5 rounded-full',
          tone === 'positive' && 'bg-positive',
          tone === 'negative' && 'bg-destructive',
          tone === 'neutral' && 'bg-muted-foreground/50'
        )}
      />
    </span>
  )
}
