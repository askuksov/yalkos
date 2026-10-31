import { type ReactNode, useEffect, useRef, useState } from 'react'

import { Kbd } from './Kbd'
import { Popover } from './Popover'
import s from './ui.module.css'

export type MenuItem = {
  label: string
  icon?: ReactNode
  shortcut?: string
  onSelect: () => void
  danger?: boolean
  disabled?: boolean
}

type Props = {
  anchor: HTMLElement | null
  open: boolean
  onClose: () => void
  items: MenuItem[]
  label: string
}

/** Action menu in a popover: arrow keys move, Enter selects, Esc closes. */
export function Menu({ anchor, open, onClose, items, label }: Props) {
  return (
    <Popover anchor={anchor} open={open} onClose={onClose} placement="bottom-end">
      <MenuList items={items} label={label} onClose={onClose} />
    </Popover>
  )
}

/** Mounted on each opening, so the first item starts active. */
function MenuList({ items, label, onClose }: Pick<Props, 'items' | 'label' | 'onClose'>) {
  const [active, setActive] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => list.current?.focus(), [])

  const select = (i: number) => {
    const item = items[i]
    if (!item || item.disabled) return
    onClose()
    item.onSelect()
  }

  return (
    <div
        ref={list}
        role="menu"
        aria-label={label}
        tabIndex={-1}
        className={s.menu}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setActive((a) => (a + 1) % items.length)
          } else if (e.key === 'ArrowUp') {
            e.preventDefault()
            setActive((a) => (a - 1 + items.length) % items.length)
          } else if (e.key === 'Enter') {
            e.preventDefault()
            select(active)
          }
        }}
      >
        {items.map((item, i) => (
          <div
            key={item.label}
            role="menuitem"
            aria-disabled={item.disabled || undefined}
            className={[s.menuItem, i === active ? s.active : '', item.danger ? s.dangerText : ''].join(' ')}
            onMouseEnter={() => setActive(i)}
            onClick={() => select(i)}
          >
            {item.icon}
            <span className={s.grow}>{item.label}</span>
            {item.shortcut && <Kbd>{item.shortcut}</Kbd>}
          </div>
        ))}
    </div>
  )
}
