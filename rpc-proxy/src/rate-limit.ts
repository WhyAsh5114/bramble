/** Fixed-window per-key counter, in memory. */
export class RateLimiter {
  private readonly limit: number
  private readonly windowMs: number
  private readonly windows = new Map<string, { start: number; count: number }>()

  constructor(limitPerWindow: number, windowMs = 60_000) {
    this.limit = limitPerWindow
    this.windowMs = windowMs
  }

  /** Returns true if the call is within the limit. A limit of 0 disables limiting. */
  allow(key: string, now = Date.now()): boolean {
    if (this.limit === 0) return true
    if (this.windows.size > 10_000) this.sweep(now)
    const w = this.windows.get(key)
    if (!w || now - w.start >= this.windowMs) {
      this.windows.set(key, { start: now, count: 1 })
      return true
    }
    w.count++
    return w.count <= this.limit
  }

  private sweep(now: number): void {
    for (const [key, w] of this.windows) if (now - w.start >= this.windowMs) this.windows.delete(key)
  }
}
