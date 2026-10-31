import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import s from '../app/app.module.css'
import { useTitle } from '../app/useTitle'

/** Card layout for pages outside the app shell (sign-in, setup, invite). */
export function Layout({ title, children }: { title: string; children: ReactNode }) {
  const { t } = useTranslation()
  useTitle(title)
  return (
    <main className={s.auth}>
      <div className={s.card}>
        <span className="brand">{t('app.name')}</span>
        <h1>{title}</h1>
        {children}
      </div>
    </main>
  )
}
