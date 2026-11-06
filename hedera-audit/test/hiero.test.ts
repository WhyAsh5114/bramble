import { describe, expect, it } from 'vitest'
import { createHieroTopicClient } from '../src/hiero.ts'

describe('createHieroTopicClient', () => {
  it('refuses mainnet before touching any key', () => {
    expect(() =>
      createHieroTopicClient({ accountId: '0.0.1', privateKey: 'x', network: 'mainnet' as 'testnet' })
    ).toThrow(/testnet and previewnet only/)
  })
})
