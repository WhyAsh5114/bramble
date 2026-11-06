import { describe, expect, it } from 'bun:test'
import { errorText, scrub, secretValues } from './redact'

describe('scrub', () => {
  it('replaces URLs that may carry an API key', () => {
    const text =
      'HTTP request failed. URL: https://sepolia.example.io/v3/0123456789abcdef0123456789abcdef\nDetails: 429'
    expect(scrub(text)).toBe('HTTP request failed. URL: <url>\nDetails: 429')
  })

  it('replaces exact secret values of any length and ignores empty ones', () => {
    expect(scrub('key abcdefgh12345678 used', ['abcdefgh12345678', undefined, ''])).toBe('key <redacted> used')
    expect(scrub('failure demo123', ['demo123'])).toBe('failure <redacted>')
    expect(scrub('ab is hidden', ['ab'])).toBe('<redacted> is hidden')
  })

  it('handles non-Error values', () => {
    expect(errorText('plain failure at http://127.0.0.1:7890/x')).toBe('plain failure at <url>')
  })

  it('removes a credential after a ")" or "," inside a URL', () => {
    expect(scrub('see https://rpc.example/v3/first)SYNTHETIC_CREDENTIAL_SUFFIX here')).toBe('see <url> here')
    expect(scrub('at https://rpc.example/a,b-secret')).toBe('at <url>')
  })

  it('removes key-shaped hex without being told the value', () => {
    const key = 'ab'.repeat(32)
    expect(scrub(`bad key 0x${key} and ${key}`)).toBe('bad key <redacted> and <redacted>')
    expect(scrub('der 302e020100300506032b657004220420' + 'cd'.repeat(32))).toBe('der <redacted>')
  })

  it('removes Ed25519 and ECDSA DER keys, with or without 0x, by shape', () => {
    const ed = '302e020100300506032b657004220420' + 'cd'.repeat(32)
    const ecdsa = '3030020100300706052b8104000a04220420' + 'ab'.repeat(32)
    const ecdsaPublic = '3056301006072a8648ce3d020106052b8104000a034200' + '04' + 'ef'.repeat(64)
    for (const key of [ed, ecdsa, ecdsaPublic]) {
      expect(scrub(`k ${key} z`)).toBe('k <redacted> z')
      expect(scrub(`k 0x${key} z`)).toBe('k <redacted> z')
    }
    expect(scrub(`glued prefix${'ab'.repeat(32)}`)).toBe('glued prefix<redacted>')
  })

  it('removes a secret value that contains a URL, whole', () => {
    const rpc = 'https://rpc.example/v3/abc)tail'
    expect(scrub(`failed ${rpc}`, [rpc])).toBe('failed <redacted>')
  })
})

describe('secretValues', () => {
  it('collects values of secret-named entries only', () => {
    const values = secretValues(
      { SEPOLIA_RPC_URL: 'u1', BRAMBLE_PRIVATE_KEY: 'k1', BRAMBLE_NODE_LABEL: 'gateway' },
      undefined,
      {
        APPROVAL_DEMO_TOKEN: 't1',
        SEPOLIA_PK: 'p1',
      }
    )
    expect(values.sort()).toEqual(['k1', 'p1', 't1', 'u1'])
  })
})
