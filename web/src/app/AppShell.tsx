import { PanelLeft } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Outlet } from 'react-router'

import { useHotkey } from '../lib/hotkeys'
import { Button } from '../ui/Button'
import s from './app.module.css'
import { Sidebar } from './Sidebar'

const sidebarKey = 'yalkos.sidebar'

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(sidebarKey) === 'collapsed'
  } catch {
    return false
  }
}

/** Sidebar + routed main area; [ collapses the sidebar. */
export function AppShell() {
  const { t } = useTranslation()
  const [collapsed, setCollapsed] = useState(readCollapsed)

  const toggle = () =>
    setCollapsed((c) => {
      try {
        localStorage.setItem(sidebarKey, c ? 'expanded' : 'collapsed')
      } catch {
        // Not stored: the toggle still works for this page view.
      }
      return !c
    })

  useHotkey('[', toggle)

  return (
    <div className={[s.shell, collapsed ? s.collapsed : ''].join(' ')}>
      {collapsed ? (
        <Button variant="ghost" className={s.expand} icon={<PanelLeft size={16} aria-hidden />} aria-label={t('nav.expandSidebar')} onClick={toggle} />
      ) : (
        <Sidebar />
      )}
      <main className={s.main}>
        <Outlet />
      </main>
    </div>
  )
}
