import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import { pseudoCatalog } from './pseudo'
import { en, namespaces } from './resources'

/** The UI locale: English in this version; dev builds add ?lng=pseudo. */
export function currentLocale(): string {
  if (import.meta.env.DEV && typeof location !== 'undefined' && new URLSearchParams(location.search).get('lng') === 'pseudo') {
    return 'pseudo'
  }
  return 'en'
}

/** Locale for Intl formatters (the pseudo-locale formats like English). */
function intlLocale(): string {
  const l = currentLocale()
  return l === 'pseudo' ? 'en' : l
}

/** Initializes i18next synchronously with the bundled catalogs. */
export function initI18n(): void {
  const lng = currentLocale()
  const resources: Record<string, Record<string, object>> = { en }
  if (import.meta.env.DEV) {
    resources.pseudo = Object.fromEntries(Object.entries(en).map(([ns, c]) => [ns, pseudoCatalog(c)]))
  }
  void i18n.use(initReactI18next).init({
    resources,
    lng,
    fallbackLng: 'en',
    ns: namespaces,
    defaultNS: 'common',
    interpolation: { escapeValue: false }, // React escapes
    returnNull: false,
    initAsync: false,
  })
  document.documentElement.lang = lng === 'pseudo' ? 'en' : lng
}

export function formatNumber(n: number): string {
  return new Intl.NumberFormat(intlLocale()).format(n)
}

let collator: Intl.Collator | undefined

/** Compares names for sorting in the UI locale. */
export function compareNames(a: string, b: string): number {
  collator ??= new Intl.Collator(intlLocale())
  return collator.compare(a, b)
}

export { i18n }
