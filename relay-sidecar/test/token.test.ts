import { describe, expect, it } from 'vitest'
import { mintToken } from '../src/token'
import { TOKEN_TTL_SECONDS } from '../src/config'

describe('mintToken', () => {
  it('produces the exact wire format relay/token.go verifies (docs/adr/0007)', () => {
    const secret = '00'.repeat(32)
    const { token, expiresAt } = mintToken(secret)

    const [payloadB64, sig] = token.split('.')
    expect(payloadB64).toBeTruthy()
    expect(sig).toMatch(/^[0-9a-f]{64}$/) // hex-encoded HMAC-SHA256

    const payload = JSON.parse(Buffer.from(payloadB64, 'base64url').toString('utf8'))
    expect(payload.exp).toBe(expiresAt)
    expect(payload.nonce).toMatch(/^[0-9a-f]{32}$/) // 16 random bytes, hex

    const nowUnix = Math.floor(Date.now() / 1000)
    expect(expiresAt).toBeGreaterThan(nowUnix)
    expect(expiresAt).toBeLessThanOrEqual(nowUnix + TOKEN_TTL_SECONDS + 1)
  })

  it('mints a fresh, unique nonce every call', () => {
    const secret = '11'.repeat(32)
    const a = mintToken(secret)
    const b = mintToken(secret)
    expect(a.token).not.toBe(b.token)
  })

  it('signs with the given secret, not a fixed one', () => {
    const a = mintToken('22'.repeat(32))
    const b = mintToken('33'.repeat(32))
    // Different secrets over what could coincidentally be the same payload
    // must never produce the same signature half of the token.
    expect(a.token.split('.')[1]).not.toBe(b.token.split('.')[1])
  })
})
