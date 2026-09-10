import { describe, expect, it } from 'vitest'
import { payForDataRelaySession } from '../src/payments/datarelay'

describe('data-relay payment input policy', () => {
  it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1])(
    'rejects unsafe byte allotment %s before signing',
    async (bytes) => {
      await expect(payForDataRelaySession('https://relay.example', bytes)).rejects.toThrow(
        'data-relay session bytes must be a positive safe integer'
      )
    }
  )
})
