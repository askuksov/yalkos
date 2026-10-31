import type { ReactNode } from 'react'

import s from './app.module.css'
import { useTitle } from './useTitle'

/** Main-area header: scope title on the left, actions on the right. */
export function PageHeader({ title, prefix, children }: { title: string; prefix?: ReactNode; children?: ReactNode }) {
  useTitle(title)
  return (
    <header className={s.header}>
      {prefix}
      <h1>{title}</h1>
      <div className={s.headerActions}>{children}</div>
    </header>
  )
}
