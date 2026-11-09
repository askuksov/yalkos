import { useCallback, useEffect, useRef, useState } from 'react'

/** Copies through a temporary off-screen textarea; works where navigator.clipboard is missing. */
function copyWithSelection(text: string): boolean {
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.setAttribute('aria-hidden', 'true')
  area.style.position = 'fixed'
  area.style.top = '-1000px'
  area.style.opacity = '0'
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
  document.body.appendChild(area)
  try {
    area.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
    previous?.focus()
  }
}

/** Writes text to the clipboard, falling back to the selection API. Resolves false when both fail. */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Denied or unavailable: try the fallback.
  }
  return copyWithSelection(text)
}

/** Copy handler, a flag that is true after the last attempt failed, and a way to clear it. */
export function useCopy() {
  const [failed, setFailed] = useState(false)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const copy = useCallback(async (text: string) => {
    const ok = await copyText(text)
    if (mounted.current) setFailed(!ok)
  }, [])
  const reset = useCallback(() => setFailed(false), [])
  return { copy, failed, reset }
}
