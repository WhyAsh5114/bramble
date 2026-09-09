import { describe, expect, it, beforeEach } from 'vitest'
import { priceForBytes } from '../src/pricing'

describe('priceForBytes', () => {
  beforeEach(() => {
    process.env.DATA_RELAY_PRICE_PER_BYTE = '1000' // 1000 atomic USDC per byte, for a legible test
  })

  it('scales linearly with requested bytes (docs/05_BUILD_PLAN.md Gate 4.2)', () => {
    const small = priceForBytes(10)
    const large = priceForBytes(10_000)
    expect(BigInt(large.amount)).toBeGreaterThan(BigInt(small.amount))
    expect(small.amount).toBe('10000')
    expect(large.amount).toBe('10000000')
  })

  it('uses testnet USDC as the asset', () => {
    expect(priceForBytes(1).asset).toBe('0.0.429274')
  })

  it('rejects non-positive or non-integer byte counts', () => {
    expect(() => priceForBytes(0)).toThrow()
    expect(() => priceForBytes(-5)).toThrow()
    expect(() => priceForBytes(1.5)).toThrow()
  })
})
