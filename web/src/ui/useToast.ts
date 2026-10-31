import { createContext, useContext } from 'react'

export type ToastOptions = { kind?: 'info' | 'error'; action?: { label: string; onClick: () => void } }
export type ToastApi = { show: (message: string, opts?: ToastOptions) => void }

export const ToastContext = createContext<ToastApi | null>(null)

export function useToast(): ToastApi {
  const api = useContext(ToastContext)
  if (!api) throw new Error('useToast outside ToastProvider')
  return api
}
