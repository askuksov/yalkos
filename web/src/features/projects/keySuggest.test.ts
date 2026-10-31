import { describe, expect, it } from 'vitest'

import { keyPattern, normalizeKey, suggestKey } from './keySuggest'

describe('suggestKey', () => {
  it('suggestKey makes initials and avoids taken keys', () => {
    expect(suggestKey('Yalkos Web App', new Set())).toBe('YWA')
    expect(suggestKey('Yalkos', new Set())).toBe('YAL')
    expect(suggestKey('Yalkos', new Set(['YAL']))).toBe('YAL2')
    expect(suggestKey('Yalkos', new Set(['YAL', 'YAL2']))).toBe('YAL3')
    expect(suggestKey('123 go', new Set())).toBe('P1G')
    expect(suggestKey('  my   big  new project ', new Set())).toBe('MBN')
    expect(suggestKey('', new Set())).toBe('P')
    for (const k of ['YWA', 'YAL2', 'P1G', 'MBN', 'P']) expect(keyPattern.test(k)).toBe(true)
  })

  it('normalizes typed keys', () => {
    expect(normalizeKey('ylk-2 x')).toBe('YLK2X')
    expect(normalizeKey('abcdefghij')).toBe('ABCDEFG')
  })
})
