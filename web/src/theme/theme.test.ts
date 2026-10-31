import { afterEach, describe, expect, it, vi } from 'vitest'

import { applyTheme, getTheme, setTheme } from './theme'

function mockMatchMedia(dark: boolean) {
  vi.stubGlobal('matchMedia', (q: string) => ({
    matches: dark && q.includes('dark'),
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('theme', () => {
  it('resolves system theme from prefers-color-scheme', () => {
    mockMatchMedia(true)
    applyTheme('system')
    expect(document.documentElement.dataset.theme).toBe('dark')
    mockMatchMedia(false)
    applyTheme('system')
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('persists an explicit theme', () => {
    mockMatchMedia(true)
    setTheme('light')
    expect(localStorage.getItem('yalkos.theme')).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(getTheme()).toBe('light')
  })

  it('survives broken storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(getTheme()).toBe('system')
    mockMatchMedia(false)
    expect(() => setTheme('dark')).not.toThrow()
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('ignores an unknown stored value', () => {
    localStorage.setItem('yalkos.theme', 'purple')
    expect(getTheme()).toBe('system')
  })
})
