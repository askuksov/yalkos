import { i18n } from '../../i18n'

/** Markdown link text with brackets and backslashes escaped. */
export function escapeText(s: string): string {
  return s.replace(/[\\[\]]/g, (c) => `\\${c}`)
}

const reEscape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

// An escaped file name on one line: no bare brackets, backslashes only as escapes.
const NAME = String.raw`(?:\\[\\[\]]|[^\\[\]\n])*`

/** Matches the upload placeholder of the current locale; null if the template has no name. */
function placeholderPattern(): string | null {
  const mark = 'YALKOS_NAME_MARK'
  const parts = i18n.t('issue:editor.uploading', { name: mark }).split(mark)
  if (parts.length !== 2) return null
  return reEscape(parts[0]) + NAME + reEscape(parts[1])
}

/**
 * Removes upload placeholders left in a saved draft: their uploads died with
 * the page. Placeholders inserted together are joined by line breaks; those
 * go with them. Line breaks around the block stay.
 */
export function dropUploadPlaceholders(text: string): string {
  const ph = placeholderPattern()
  if (!ph) return text
  return text.replace(new RegExp(`${ph}(?:\\n${ph})*`, 'g'), '')
}
