import { useEffect, useRef } from 'react'

// Shortcuts match physical keys (KeyboardEvent.code), so they work with any
// keyboard layout: a Russian layout types "с" on KeyC and still fires "c".

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)

type Combo = { code: string; mod: boolean; shift: boolean; alt: boolean }

const named: Record<string, string> = {
  '/': 'Slash', '?': 'Slash', '[': 'BracketLeft', ']': 'BracketRight', '.': 'Period', ',': 'Comma',
  enter: 'Enter', escape: 'Escape', backspace: 'Backspace', space: 'Space',
  arrowup: 'ArrowUp', arrowdown: 'ArrowDown', arrowleft: 'ArrowLeft', arrowright: 'ArrowRight',
}

function parse(key: string): Combo {
  const parts = key.toLowerCase().split('+')
  const base = parts[parts.length - 1]
  const mods = new Set(parts.slice(0, -1))
  let code = named[base]
  if (!code && /^[a-z]$/.test(base)) code = `Key${base.toUpperCase()}`
  if (!code && /^[0-9]$/.test(base)) code = `Digit${base}`
  if (!code) throw new Error(`unsupported hotkey "${key}"`)
  return { code, mod: mods.has('mod'), shift: mods.has('shift') || base === '?', alt: mods.has('alt') }
}

export function matches(e: KeyboardEvent, key: string): boolean {
  const c = parse(key)
  const mod = isMac ? e.metaKey : e.ctrlKey
  const other = isMac ? e.ctrlKey : e.metaKey
  return e.code === c.code && mod === c.mod && !other && e.shiftKey === c.shift && e.altKey === c.alt
}

export function isEditable(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false
  if (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT') return true
  return t.isContentEditable || t.closest('[contenteditable]:not([contenteditable="false"])') !== null
}

function dialogOpen(): boolean {
  return document.querySelector('dialog[open]') !== null
}

type Options = { enabled?: boolean; allowInInputs?: boolean }

/**
 * Runs handler on a key combo such as 'c', 'mod+k' (Meta on macOS, Ctrl
 * elsewhere), 'shift+/' or '?'. Skips typing targets and open dialogs unless
 * allowInInputs.
 */
export function useHotkey(key: string, handler: (e: KeyboardEvent) => void, { enabled = true, allowInInputs = false }: Options = {}) {
  const ref = useRef(handler)
  useEffect(() => {
    ref.current = handler
  })
  useEffect(() => {
    if (!enabled) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || !matches(e, key)) return
      if (!allowInInputs && (isEditable(e.target) || dialogOpen())) return
      e.preventDefault()
      ref.current(e)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [key, enabled, allowInInputs])
}

// Esc handlers form a stack: only the most recently registered one runs.
// Dialogs and popovers handle Esc themselves and stop it before it gets here.
const escStack: { current: () => void }[] = []

function onEscape(e: KeyboardEvent) {
  if (e.key !== 'Escape' || e.defaultPrevented || escStack.length === 0) return
  if (dialogOpen()) return
  e.preventDefault()
  escStack[escStack.length - 1].current()
}

export function useEscape(handler: () => void, enabled = true) {
  const ref = useRef(handler)
  useEffect(() => {
    ref.current = handler
  })
  useEffect(() => {
    if (!enabled) return
    const entry = { current: () => ref.current() }
    if (escStack.length === 0) window.addEventListener('keydown', onEscape)
    escStack.push(entry)
    return () => {
      escStack.splice(escStack.indexOf(entry), 1)
      if (escStack.length === 0) window.removeEventListener('keydown', onEscape)
    }
  }, [enabled])
}

/** Label for a combo in the US layout, e.g. "⌘K" or "Ctrl+K". */
export function shortcutLabel(key: string): string {
  const parts = key.split('+')
  const base = parts[parts.length - 1]
  const label = base.length === 1 ? base.toUpperCase() : base
  if (!parts.includes('mod')) return label
  return isMac ? `⌘${label}` : `Ctrl+${label}`
}
