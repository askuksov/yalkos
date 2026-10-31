import { X } from 'lucide-react'
import { type ReactNode, useEffect, useId, useLayoutEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import s from './ui.module.css'

type Props = {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  /** Wide layout (create issue, move). */
  wide?: boolean
  /** No visible header (the title stays for screen readers), e.g. the command palette. */
  bare?: boolean
}

/**
 * Modal dialog on the native <dialog>: Esc and a backdrop click call onClose,
 * focus returns to the element that had it before opening. Children mount only
 * while open, so each opening starts fresh.
 */
export function Dialog({ open, onClose, title, children, wide, bare }: Props) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDialogElement>(null)
  const opener = useRef<HTMLElement | null>(null)
  const titleId = useId()
  const onCloseRef = useRef(onClose)
  useLayoutEffect(() => {
    onCloseRef.current = onClose
  })

  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
      d.showModal()
      // showModal focuses the first focusable element (the close button); prefer the first field.
      const field = d.querySelector<HTMLElement>('[data-autofocus], input:not([type="hidden"]):not([disabled]), textarea, select')
      field?.focus()
    } else if (!open && d.open) {
      d.close()
    }
    if (!open && opener.current) {
      const el = opener.current
      opener.current = null
      if (el.isConnected) el.focus()
    }
  }, [open])

  // Unmounted while open: still hand focus back.
  useEffect(
    () => () => {
      if (opener.current?.isConnected) opener.current.focus()
    },
    [],
  )

  return (
    <dialog
      ref={ref}
      className={[s.dialog, wide ? s.dialogWide : '', bare ? s.dialogBare : ''].join(' ')}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault()
        onCloseRef.current()
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && !e.defaultPrevented) {
          e.preventDefault()
          e.stopPropagation()
          onCloseRef.current()
        }
      }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCloseRef.current()
      }}
    >
      {open && (
        <div className={s.dialogBody}>
          <header className={bare ? s.srOnly : s.dialogHeader}>
            <h2 id={titleId}>{title}</h2>
            <button type="button" className={s.close} aria-label={t('actions.close')} onClick={() => onCloseRef.current()}>
              <X size={16} aria-hidden />
            </button>
          </header>
          {children}
        </div>
      )}
    </dialog>
  )
}
