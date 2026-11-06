import { describe, expect, it } from 'bun:test'
import { formatPlan, planLaunch, startUnits, type RunCommand } from './launch'

const OPTS = { relayInstances: ['relay1'], natBox: false }

describe('planLaunch', () => {
  it('lists local units in start order and the other roles as instructions', () => {
    const plan = planLaunch(['worker', 'gateway'], OPTS)
    expect(plan.here.map((h) => h.role)).toEqual(['gateway', 'worker'])
    expect(plan.here[0].units).toEqual(['bramble-demo-service', 'bramble-gateway'])
    expect(plan.elsewhere.map((e) => e.role)).toEqual(['relays', 'admin'])
    expect(plan.elsewhere[0].command).toBe('pnpm demo --role relays')
  })

  it('starts the NAT box first when nodes run in it', () => {
    const plan = planLaunch(['worker'], { ...OPTS, natBox: true })
    expect(plan.here[0].units).toEqual(['bramble-nat-box', 'bramble-worker'])
  })

  it('names one relay unit per instance', () => {
    const plan = planLaunch(['relays'], { relayInstances: ['a', 'b'], natBox: false })
    expect(plan.here[0].units).toEqual(['bramble-relay@a', 'bramble-relay@b'])
  })

  it('a single machine with every role has nothing to start elsewhere', () => {
    const plan = planLaunch(['relays', 'gateway', 'worker', 'admin'], OPTS)
    expect(plan.elsewhere).toEqual([])
    expect(formatPlan(plan)).not.toContain('other machines')
  })

  it('prints the relays support units the launcher does not start', () => {
    const plan = planLaunch(['relays'], OPTS)
    expect(plan.here[0].units.some((u) => u === 'caddy' || u === 'coturn' || u === 'rpc-proxy')).toBe(false)
    const text = formatPlan(plan)
    expect(text).toContain('sudo systemctl enable --now rpc-proxy caddy coturn')
    expect(text).toContain('Not started by this command')
  })

  it('the printed plan names the other machines', () => {
    const text = formatPlan(planLaunch(['gateway'], OPTS))
    expect(text).toContain('pnpm demo --role worker')
    expect(text).toContain('bramble-demo-service -> bramble-gateway')
  })
})

function fakeRun(over: { failStart?: string; inactive?: string } = {}) {
  const calls: string[] = []
  const run: RunCommand = async (file, args) => {
    calls.push([file, ...args].join(' '))
    const unit = args.at(-1) as string
    if (args.includes('start')) return { code: unit === over.failStart ? 1 : 0, stdout: '' }
    if (args.includes('is-active')) return { code: 0, stdout: unit === over.inactive ? 'activating\n' : 'active\n' }
    return { code: 0, stdout: '' }
  }
  return { run, calls }
}

describe('startUnits', () => {
  it('starts every unit in order through sudo, then checks it is active', async () => {
    const plan = planLaunch(['gateway'], OPTS)
    const { run, calls } = fakeRun()
    const result = await startUnits(plan, run, { asRoot: false, log: () => {} })
    expect(result.started).toEqual(['bramble-demo-service', 'bramble-gateway'])
    expect(calls).toEqual([
      'sudo systemctl start bramble-demo-service',
      'systemctl is-active bramble-demo-service',
      'sudo systemctl start bramble-gateway',
      'systemctl is-active bramble-gateway',
    ])
  })

  it('skips sudo when already root', async () => {
    const { run, calls } = fakeRun()
    await startUnits(planLaunch(['worker'], OPTS), run, { asRoot: true, log: () => {} })
    expect(calls[0]).toBe('systemctl start bramble-worker')
  })

  it('stops at the first failure and does not start later units', async () => {
    const { run, calls } = fakeRun({ failStart: 'bramble-demo-service' })
    const result = await startUnits(planLaunch(['gateway'], OPTS), run, { asRoot: true, log: () => {} })
    expect(result.failed?.unit).toBe('bramble-demo-service')
    expect(result.failed?.reason).toContain('journalctl -u bramble-demo-service')
    expect(calls.some((c) => c.includes('bramble-gateway'))).toBe(false)
  })

  it('reports a unit that started but is not active', async () => {
    const { run } = fakeRun({ inactive: 'bramble-worker' })
    const result = await startUnits(planLaunch(['worker'], OPTS), run, { asRoot: true, log: () => {} })
    expect(result.failed?.reason).toContain('activating')
  })
})
