import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, ChevronDown, ChevronRight, CircleUser, Compass, KeyRound, LogOut, Plus, Users } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { NavLink, useLocation, useNavigate } from 'react-router'

import { request } from '../api/client'
import { useIsAdmin, useProjects, useViewer } from '../api/hooks'
import { LogoutMutation } from '../api/operations'
import { startSession } from '../auth/session'
import { useCreateIssue } from '../features/issues/createIssue'
import { compareNames } from '../i18n'
import { getTheme, setTheme, type ThemeSetting } from '../theme/theme'
import { Avatar } from '../ui/Avatar'
import { Kbd } from '../ui/Kbd'
import { hueOf } from '../ui/avatarColor'
import { Menu } from '../ui/Menu'
import s from './app.module.css'

export function Sidebar() {
  const { t } = useTranslation(['common', 'issues'])
  const projects = useProjects()
  const isAdmin = useIsAdmin()
  const { pathname } = useLocation()
  const mine = (projects.data ?? []).filter((p) => p.isMember).sort((a, b) => compareNames(a.name, b.name))
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const { openForPage } = useCreateIssue()

  return (
    <aside className={s.sidebar}>
      <div className={s.workspace}>{t('app.name')}</div>
      <button type="button" className={s.navButton} onClick={openForPage}>
        <Plus size={15} aria-hidden />
        <span className={s.navLabel}>{t('issues:create.button')}</span>
        <Kbd>C</Kbd>
      </button>
      <nav aria-label={t('nav.main')} className={s.nav}>
        <NavLink to="/my-issues" className={s.navLink}>
          <CircleUser size={15} aria-hidden />
          <span className={s.navLabel}>{t('nav.myIssues')}</span>
        </NavLink>

        <div className={s.section}>{t('nav.projects')}</div>
        {mine.map((p) => {
          const base = `/p/${p.key}`
          const active = pathname === base || pathname.startsWith(`${base}/`)
          const expanded = open[p.id] ?? active
          return (
            <div key={p.id}>
              <div className={s.projectRow}>
                <button
                  type="button"
                  className={s.chevron}
                  aria-expanded={expanded}
                  aria-label={t('nav.expandProject', { name: p.name })}
                  onClick={() => setOpen({ ...open, [p.id]: !expanded })}
                >
                  {expanded ? <ChevronDown size={14} aria-hidden /> : <ChevronRight size={14} aria-hidden />}
                </button>
                <NavLink to={base} end className={s.navLink} style={{ flex: 1 }}>
                  <span className={s.dot} style={{ background: p.color ?? `hsl(${hueOf(p.id)} 45% 50%)` }} aria-hidden />
                  <span className={s.navLabel}>{p.name}</span>
                  <span className={s.projectKey}>{p.key}</span>
                </NavLink>
              </div>
              {expanded && (
                <div className={s.sub}>
                  <NavLink to={base} end className={s.navLink}>
                    {t('nav.projectIssues')}
                  </NavLink>
                  {isAdmin && (
                    <NavLink to={`${base}/settings`} className={s.navLink}>
                      {t('nav.projectSettings')}
                    </NavLink>
                  )}
                </div>
              )}
            </div>
          )
        })}
        <NavLink to="/projects" className={s.navLink}>
          <Compass size={15} aria-hidden />
          <span className={s.navLabel}>{t('nav.browseProjects')}</span>
        </NavLink>

        <div className={s.section}>{t('nav.settings')}</div>
        <NavLink to="/settings/members" className={s.navLink}>
          <Users size={15} aria-hidden />
          <span className={s.navLabel}>{t('nav.members')}</span>
        </NavLink>
        <NavLink to="/settings/api-keys" className={s.navLink}>
          <KeyRound size={15} aria-hidden />
          <span className={s.navLabel}>{t('nav.apiKeys')}</span>
        </NavLink>
      </nav>
      <div className={s.spacer} />
      <UserMenu />
    </aside>
  )
}

function UserMenu() {
  const { t } = useTranslation()
  const viewer = useViewer().data
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const [open, setOpen] = useState(false)
  const [theme, setThemeState] = useState<ThemeSetting>(getTheme)
  const logout = useMutation({
    mutationFn: () => request(LogoutMutation),
    onSuccess: () => {
      startSession()
      qc.clear()
      navigate('/login', { replace: true })
    },
  })
  if (!viewer) return null
  const pick = (v: ThemeSetting) => () => {
    setTheme(v)
    setThemeState(v)
  }
  const themeItem = (v: ThemeSetting) => ({
    label: `${t('theme.label')}: ${t(`theme.${v}`)}`,
    icon: theme === v ? <Check size={14} aria-hidden /> : <span style={{ inlineSize: 14 }} />,
    onSelect: pick(v),
  })
  return (
    <>
      <button ref={setAnchor} type="button" className={s.navButton} aria-haspopup="menu" onClick={() => setOpen(!open)}>
        <Avatar id={viewer.id} name={viewer.name} />
        <span className={s.navLabel}>{viewer.name}</span>
      </button>
      <Menu
        anchor={anchor}
        open={open}
        onClose={() => setOpen(false)}
        label={t('nav.userMenu')}
        items={[
          themeItem('system'),
          themeItem('light'),
          themeItem('dark'),
          { label: t('nav.logOut'), icon: <LogOut size={14} aria-hidden />, onSelect: () => logout.mutate() },
        ]}
      />
    </>
  )
}
