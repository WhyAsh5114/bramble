export interface LogEntry {
  /** Monotonic sequence number, starting at 1. Survives ring eviction, not restart. */
  seq: number
  /** ISO 8601 UTC. */
  time: string
  sourceIp: string
  method: string
  /** JSON-encoded params, truncated to the configured limit. */
  params: string
}

export interface LogView {
  entries: LogEntry[]
  /** Sequence number of the newest entry ever added (0 if none). Pass as `since` to receive only newer entries. */
  lastSeq: number
  /** Total entries dropped by ring eviction since start. */
  dropped: number
}

/** Fixed-capacity in-memory ring. Nothing here touches disk. */
export class MemoryLog {
  private readonly capacity: number
  private entries: LogEntry[] = []
  private seq = 0
  private dropped = 0

  constructor(capacity: number) {
    if (capacity < 1) throw new Error('log capacity must be at least 1')
    this.capacity = capacity
  }

  add(entry: Omit<LogEntry, 'seq'>): LogEntry {
    const full = { seq: ++this.seq, ...entry }
    this.entries.push(full)
    if (this.entries.length > this.capacity) {
      this.entries.shift()
      this.dropped++
    }
    return full
  }

  /** Entries with seq greater than `since`, oldest first, at most `limit` (the newest ones). */
  view(since = 0, limit = this.capacity): LogView {
    const newer = this.entries.filter((e) => e.seq > since)
    const entries = limit >= newer.length ? newer : newer.slice(newer.length - limit)
    return { entries, lastSeq: this.seq, dropped: this.dropped }
  }
}

export function truncate(text: string, max: number): string {
  return text.length <= max ? text : text.slice(0, max) + `…[truncated ${text.length - max} chars]`
}
