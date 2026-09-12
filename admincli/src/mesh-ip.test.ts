import { describe, expect, it } from 'bun:test'
import { allocateMeshIP, MESH_NETWORK_PREFIX, MESH_PREFIX_BITS } from './mesh-ip'

describe('allocateMeshIP', () => {
  it('picks .2 on an empty tailnet', () => {
    expect(allocateMeshIP([])).toBe(`${MESH_NETWORK_PREFIX}2/${MESH_PREFIX_BITS}`)
  })

  it('skips already-used hosts, including nulls from never-enrolled records', () => {
    expect(allocateMeshIP([null, `${MESH_NETWORK_PREFIX}2/24`, undefined, `${MESH_NETWORK_PREFIX}3/24`])).toBe(
      `${MESH_NETWORK_PREFIX}4/24`
    )
  })

  it('fills a gap left by a lower unused host rather than always appending', () => {
    expect(allocateMeshIP([`${MESH_NETWORK_PREFIX}2/24`, `${MESH_NETWORK_PREFIX}4/24`])).toBe(
      `${MESH_NETWORK_PREFIX}3/24`
    )
  })

  it('ignores foreign/unparseable values instead of colliding on them', () => {
    expect(allocateMeshIP(['not-an-ip', '192.168.1.5/24', ''])).toBe(`${MESH_NETWORK_PREFIX}2/${MESH_PREFIX_BITS}`)
  })

  it('throws once the /24 is exhausted rather than silently reusing a host', () => {
    const allUsed = Array.from({ length: 253 }, (_, i) => `${MESH_NETWORK_PREFIX}${i + 2}/24`)
    expect(() => allocateMeshIP(allUsed)).toThrow(/exhausted/)
  })
})
