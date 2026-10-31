import { type ReactNode, useId, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import s from './ui.module.css'

export type PickerProps<T> = {
  items: T[]
  value?: T | null
  getKey: (item: T) => string
  getLabel: (item: T) => string
  renderItem?: (item: T) => ReactNode
  onSelect: (item: T) => void
  onClose: () => void
  placeholder: string
  /** Server-side filtering: called with the input; items are shown as given. */
  onQuery?: (q: string) => void
  /** Multi-select: which items are chosen (instead of value). */
  isSelected?: (item: T) => boolean
}

/**
 * Combobox: type to filter (case-insensitive substring of the label), arrows
 * move, Enter selects, Esc closes; the current value is aria-selected.
 */
export function Picker<T>({ items, value, getKey, getLabel, renderItem, onSelect, onClose, placeholder, onQuery, isSelected }: PickerProps<T>) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const needle = q.trim().toLowerCase()
  const shown = onQuery || !needle ? items : items.filter((i) => getLabel(i).toLowerCase().includes(needle))
  const current = value == null ? null : getKey(value)
  const [active, setActive] = useState(() => Math.max(0, items.findIndex((i) => getKey(i) === current)))
  const listId = useId()
  const list = useRef<HTMLDivElement>(null)
  const at = Math.min(active, Math.max(shown.length - 1, 0))

  useLayoutEffect(() => {
    const el = list.current?.querySelector<HTMLElement>(`[data-index="${at}"]`)
    el?.scrollIntoView?.({ block: 'nearest' })
  }, [at])

  return (
    <div className={s.picker}>
      <input
        autoFocus
        className={s.pickerInput}
        role="combobox"
        aria-expanded
        aria-controls={listId}
        aria-activedescendant={shown.length ? `${listId}-${at}` : undefined}
        placeholder={placeholder}
        value={q}
        onChange={(e) => {
          setQ(e.target.value)
          setActive(0)
          onQuery?.(e.target.value)
        }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setActive(shown.length ? (at + 1) % shown.length : 0)
          } else if (e.key === 'ArrowUp') {
            e.preventDefault()
            setActive(shown.length ? (at - 1 + shown.length) % shown.length : 0)
          } else if (e.key === 'Enter') {
            e.preventDefault()
            if (shown[at]) onSelect(shown[at])
          } else if (e.key === 'Escape') {
            e.preventDefault()
            e.stopPropagation()
            onClose()
          }
        }}
      />
      <div ref={list} id={listId} role="listbox" aria-multiselectable={isSelected ? true : undefined} className={s.pickerList}>
        {shown.map((item, i) => (
          <div
            key={getKey(item)}
            id={`${listId}-${i}`}
            data-index={i}
            role="option"
            aria-selected={isSelected ? isSelected(item) : getKey(item) === current}
            className={[s.menuItem, i === at ? s.active : ''].join(' ')}
            onMouseEnter={() => setActive(i)}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => onSelect(item)}
          >
            {renderItem ? renderItem(item) : <span className={s.grow}>{getLabel(item)}</span>}
          </div>
        ))}
        {shown.length === 0 && <div className={s.pickerEmpty}>{t('pickers.noResults')}</div>}
      </div>
    </div>
  )
}
