import { autoUpdate, flip, offset, type Placement, shift, useFloating } from '@floating-ui/react-dom'
import { type ReactNode, useEffect, useLayoutEffect, useRef } from 'react'
import { createPortal } from 'react-dom'

import s from './ui.module.css'

type Props = {
  anchor: HTMLElement | null
  open: boolean
  onClose: () => void
  children: ReactNode
  placement?: Placement
}

/**
 * Floating panel next to anchor; closes on an outside click and on Esc. It
 * renders into the open modal dialog that holds the anchor (the rest of the
 * page is inert while a modal is open), otherwise into <body>.
 */
export function Popover({ anchor, open, onClose, children, placement = 'bottom-start' }: Props) {
  const { refs, floatingStyles } = useFloating({
    elements: { reference: anchor },
    placement,
    open,
    middleware: [offset(4), flip(), shift({ padding: 8 })],
    whileElementsMounted: autoUpdate,
  })
  const panel = useRef<HTMLDivElement | null>(null)
  const onCloseRef = useRef(onClose)
  useLayoutEffect(() => {
    onCloseRef.current = onClose
  })

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node
      if (panel.current?.contains(target) || anchor?.contains(target)) return
      onCloseRef.current()
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open, anchor])

  if (!open) return null
  const host = anchor?.closest('dialog[open]') ?? document.body
  return createPortal(
    <div
      ref={(el) => {
        panel.current = el
        refs.setFloating(el)
      }}
      className={s.popover}
      style={floatingStyles}
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.preventDefault()
          e.stopPropagation()
          onCloseRef.current()
        }
      }}
    >
      {children}
    </div>,
    host,
  )
}
