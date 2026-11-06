import { describe, expect, it } from 'bun:test'
import { parseEnvText } from './env-file'

describe('parseEnvText', () => {
  it('parses plain, quoted, exported and commented lines', () => {
    const env = parseEnvText(
      ['# comment', 'A=1', 'export B="two words"', "C='x'", 'D=value # trailing', '', 'E=', 'bad line'].join('\n')
    )
    expect(env).toEqual({ A: '1', B: 'two words', C: 'x', D: 'value', E: '' })
  })

  it('keeps an = inside a value', () => {
    expect(parseEnvText('URL=a=b').URL).toBe('a=b')
  })
})
