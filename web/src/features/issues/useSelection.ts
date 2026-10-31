import { type RefObject, useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router'

import { isInteractive, useEscape, useHotkey } from '../../lib/hotkeys'
import { usePalette } from '../command/palette'
import { targetsFrom } from '../command/targets'

/**
 * Keyboard selection over the rows rendered in container ([data-row-id],
 * [data-href]): J/K move, Enter opens, X toggles the checkbox, Esc clears the
 * checked set. Hover moves the same highlight (setSelected).
 */
export function useSelection(
  container: RefObject<HTMLElement | null>,
  { checkable = true, initialSelected = null }: { checkable?: boolean; initialSelected?: string | null } = {},
) {
  const navigate = useNavigate()
  const [selected, setSelected] = useState<string | null>(initialSelected)
  const [checked, setChecked] = useState<ReadonlySet<string>>(new Set())

  const rows = () => [...(container.current?.querySelectorAll<HTMLElement>('[data-row-id]') ?? [])]

  const move = (delta: number) => {
    const list = rows()
    if (!list.length) return
    const at = list.findIndex((r) => r.dataset.rowId === selected)
    const next = list[at < 0 ? (delta > 0 ? 0 : list.length - 1) : Math.min(Math.max(at + delta, 0), list.length - 1)]
    setSelected(next.dataset.rowId ?? null)
    next.scrollIntoView?.({ block: 'nearest' })
  }

  const toggle = useCallback((id: string) => {
    setChecked((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])
  const clear = useCallback(() => setChecked(new Set()), [])

  useHotkey('j', () => move(1))
  useHotkey('k', () => move(-1))
  useHotkey('enter', (e) => {
    if (isInteractive(e.target)) return false
    const href = rows().find((r) => r.dataset.rowId === selected)?.dataset.href
    if (!href) return false
    navigate(href)
  })
  useHotkey('x', () => {
    if (selected) toggle(selected)
  }, { enabled: checkable })
  useEscape(clear, checked.size > 0)

  // S/A/P/T/L/M: palette pages for the checked issues, or the highlighted one.
  const palette = usePalette()
  const targets = () => {
    const list = rows()
    const picked = checked.size ? list.filter((r) => checked.has(r.dataset.rowId ?? '')) : list.filter((r) => r.dataset.rowId === selected)
    return targetsFrom(picked)
  }
  const openPage = (page: 'state' | 'assignee' | 'priority' | 'type' | 'labels' | 'milestone') => {
    const t = targets()
    if (!t.length) return false
    palette.open(page, t)
  }
  useHotkey('s', () => openPage('state'), { enabled: checkable })
  useHotkey('a', () => openPage('assignee'), { enabled: checkable })
  useHotkey('p', () => openPage('priority'), { enabled: checkable })
  useHotkey('t', () => openPage('type'), { enabled: checkable })
  useHotkey('l', () => openPage('labels'), { enabled: checkable })
  useHotkey('m', () => openPage('milestone'), { enabled: checkable })
  useEffect(() => {
    palette.setPageTarget(checkable ? targets() : null)
    return () => palette.setPageTarget(null)
  })

  return { selected, setSelected, checked: checkable ? checked : new Set<string>(), toggle, clear }
}
