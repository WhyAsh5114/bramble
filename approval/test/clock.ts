import type { Clock } from '../src/lib/contracts'

export class ManualClock implements Clock {
  private time = Date.UTC(2026, 10, 6)
  private readonly tasks = new Map<symbol, { due: number; action: () => Promise<void> }>()
  now = () => this.time
  schedule(action: () => Promise<void>, delayMs: number) {
    const id = Symbol()
    this.tasks.set(id, { due: this.time + delayMs, action })
    return () => {
      this.tasks.delete(id)
    }
  }
  async advance(ms: number) {
    this.time += ms
    for (const [id, task] of [...this.tasks]) {
      if (task.due <= this.time && this.tasks.delete(id)) await task.action()
    }
  }
}
