import { X } from 'lucide-react'
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import s from './ui.module.css'
import { ToastContext, type ToastOptions } from './useToast'

type ToastItem = ToastOptions & { id: number; message: string }

const infoMs = 4000
const maxVisible = 3

/** Toasts bottom-left: info ones leave after 4 s, errors stay until closed. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const [items, setItems] = useState<ToastItem[]>([])
  const seq = useRef(0)
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>())

  const dismiss = useCallback((id: number) => {
    clearTimeout(timers.current.get(id))
    timers.current.delete(id)
    setItems((list) => list.filter((i) => i.id !== id))
  }, [])

  const show = useCallback(
    (message: string, opts: ToastOptions = {}) => {
      const id = ++seq.current
      setItems((list) => [...list, { id, message, ...opts }].slice(-maxVisible))
      if (opts.kind !== 'error') timers.current.set(id, setTimeout(() => dismiss(id), infoMs))
    },
    [dismiss],
  )

  useEffect(() => {
    const all = timers.current
    return () => all.forEach(clearTimeout)
  }, [])

  const api = useMemo(() => ({ show }), [show])
  return (
    <ToastContext.Provider value={api}>
      {children}
      <section className={s.toasts} aria-label={t('toast.region')}>
        {items.map((i) => (
          <div key={i.id} className={[s.toast, i.kind === 'error' ? s.toastError : ''].join(' ')} role={i.kind === 'error' ? 'alert' : 'status'}>
            <span className={s.grow}>{i.message}</span>
            {i.action && (
              <button
                type="button"
                className={s.toastAction}
                onClick={() => {
                  dismiss(i.id)
                  i.action?.onClick()
                }}
              >
                {i.action.label}
              </button>
            )}
            <button type="button" className={s.close} aria-label={t('actions.close')} onClick={() => dismiss(i.id)}>
              <X size={14} aria-hidden />
            </button>
          </div>
        ))}
      </section>
    </ToastContext.Provider>
  )
}
