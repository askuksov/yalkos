export type ThemeSetting = 'system' | 'light' | 'dark'

const storageKey = 'yalkos.theme'
const darkQuery = '(prefers-color-scheme: dark)'

function isSetting(v: unknown): v is ThemeSetting {
  return v === 'system' || v === 'light' || v === 'dark'
}

/** The stored theme setting; missing or unreadable storage means system. */
export function getTheme(): ThemeSetting {
  try {
    const v = localStorage.getItem(storageKey)
    return isSetting(v) ? v : 'system'
  } catch {
    return 'system'
  }
}

function systemDark(): boolean {
  return typeof matchMedia === 'function' && matchMedia(darkQuery).matches
}

function apply(t: ThemeSetting) {
  const dark = t === 'dark' || (t === 'system' && systemDark())
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
}

let stopFollowing: (() => void) | undefined

/** Stores and applies a theme; while 'system' it follows the OS setting. */
export function setTheme(t: ThemeSetting): void {
  try {
    localStorage.setItem(storageKey, t)
  } catch {
    // Storage unavailable (private mode, quota): the theme still applies.
  }
  applyTheme(t)
}

/** Applies a theme without storing it (startup). */
export function applyTheme(t: ThemeSetting = getTheme()): void {
  stopFollowing?.()
  stopFollowing = undefined
  apply(t)
  if (t === 'system' && typeof matchMedia === 'function') {
    const mq = matchMedia(darkQuery)
    const onChange = () => apply('system')
    mq.addEventListener?.('change', onChange)
    stopFollowing = () => mq.removeEventListener?.('change', onChange)
  }
}
