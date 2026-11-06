import { describe, expect, it } from 'bun:test'
import { loadEnv, probeSpecs } from './context'

describe('probeSpecs', () => {
  it('uses this machine for a local role and ssh for a remote one', () => {
    const specs = probeSpecs({ DEMO_WORKER_SSH: 'ubuntu@worker' }, ['gateway'])
    expect(specs).toEqual({ gateway: {}, worker: { ssh: 'ubuntu@worker' } })
  })

  it('leaves out a remote role with no ssh target, so the preflight fails on it', () => {
    expect(probeSpecs({}, ['gateway'])).toEqual({ gateway: {} })
  })

  it('probes local nodes inside the NAT box namespace when DEMO_NAT_BOX is set', () => {
    expect(probeSpecs({ DEMO_NAT_BOX: '1' }, ['worker'])).toEqual({ worker: { netns: 'bramble-node' } })
    expect(probeSpecs({ DEMO_NAT_BOX: '1', NATBOX_NS: 'box2' }, ['worker'])).toEqual({ worker: { netns: 'box2' } })
  })

  it('uses a per-role namespace for a remote NAT-box node', () => {
    const specs = probeSpecs({ DEMO_GATEWAY_SSH: 'gw', DEMO_GATEWAY_NETNS: 'bramble-node' }, ['worker'])
    expect(specs.gateway).toEqual({ ssh: 'gw', netns: 'bramble-node' })
  })
})

describe('loadEnv', () => {
  it('lets the process env win over the file', () => {
    expect(loadEnv({ A: 'proc', B: undefined }, { A: 'file', B: 'file-b', C: 'c' })).toEqual({
      A: 'proc',
      B: 'file-b',
      C: 'c',
    })
  })
})
