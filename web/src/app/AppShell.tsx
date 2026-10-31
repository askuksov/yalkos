import { PanelLeft } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Outlet } from 'react-router'

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

function isEditable(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false
  return t.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName)
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

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.code !== 'BracketLeft' || e.metaKey || e.ctrlKey || e.altKey || isEditable(e.target)) return
      if (document.querySelector('dialog[open]')) return
      e.preventDefault()
      toggle()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

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
