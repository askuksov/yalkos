import { describe, expect, it } from 'vitest'

import { i18n } from '../../i18n'
import { dropUploadPlaceholders, escapeText } from './placeholders'

const ph = (name: string) => i18n.t('issue:editor.uploading', { name: escapeText(name) })

describe('dropUploadPlaceholders', () => {
  it('removes a block of placeholders with the breaks that joined them', () => {
    const block = [ph('a.png'), ph('b[1].png'), ph('c\\d.png')].join('\n')
    expect(dropUploadPlaceholders(`x\n${block}\ny`)).toBe('x\n\ny')
    expect(dropUploadPlaceholders(`${block}tail`)).toBe('tail')
  })

  it('removes separate placeholders one by one', () => {
    expect(dropUploadPlaceholders(`${ph('a')} and ${ph('b')}`)).toBe(' and ')
  })

  it('matches names with escaped brackets and backslashes only on one line', () => {
    expect(dropUploadPlaceholders(ph('a]b[.png'))).toBe('')
    expect(dropUploadPlaceholders(ph('$&.png'))).toBe('')
    const broken = '![Uploading a\nb…]()'
    expect(dropUploadPlaceholders(broken)).toBe(broken)
  })

  it('leaves finished links, plain image syntax and text alone', () => {
    const text = '![a.png](/api/attachments/a1/a.png) [b](/x) ![Uploading…]() ![](y)\n\n'
    expect(dropUploadPlaceholders(text)).toBe(text)
  })
})
