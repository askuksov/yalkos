import { describe, expect, it } from 'vitest'

import { compareNames, formatNumber, i18n } from '.'

describe('translation layer', () => {
  it('translates from the English catalog', () => {
    expect(i18n.t('common:actions.save')).toBe('Save')
    expect(i18n.t('errors:reasons.too_long', { max: 255 })).toBe('Must be at most 255 characters')
    expect(document.documentElement.lang).toBe('en')
  })

  it('uses CLDR plural forms', () => {
    i18n.addResourceBundle('ru', 'test', { n_one: 'one {{count}}', n_few: 'few {{count}}', n_many: 'many {{count}}', n_other: 'other {{count}}' })
    // A test-only namespace: keys are not in the typed catalogs.
    const t = i18n.t as unknown as (key: string, opts: object) => string
    const n = (count: number) => t('test:n', { lng: 'ru', count })
    expect(n(1)).toBe('one 1')
    expect(n(3)).toBe('few 3')
    expect(n(5)).toBe('many 5')
    expect(n(21)).toBe('one 21')
  })

  it('pseudo-locale marks every string', () => {
    const save = i18n.getFixedT('pseudo', 'common')('actions.save')
    expect(save.startsWith('⟦')).toBe(true)
    expect(save.endsWith('⟧')).toBe(true)
    expect(save).not.toContain('Save')
    const withValue = i18n.getFixedT('pseudo', 'errors')('reasons.too_long', { max: 255 })
    expect(withValue).toContain('255')
    expect(withValue.startsWith('⟦')).toBe(true)
  })

  it('formats numbers and compares names by locale', () => {
    expect(formatNumber(12345)).toBe('12,345')
    expect(['b', 'Ä', 'a'].sort(compareNames)).toEqual(['a', 'Ä', 'b'])
  })
})
