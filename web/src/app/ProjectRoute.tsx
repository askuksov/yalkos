import { useTranslation } from 'react-i18next'
import { Link, Navigate, Outlet, useLocation, useParams } from 'react-router'

import { GraphQLRequestError } from '../api/client'
import { useProject } from '../api/hooks'
import s from './app.module.css'
import { useTitle } from './useTitle'

/**
 * Loads the project of /p/:key. A former key redirects to the same page under
 * the current key; an unknown key shows "Project not found".
 */
export function ProjectRoute() {
  const { t } = useTranslation()
  const { key = '' } = useParams()
  const location = useLocation()
  const project = useProject(key)

  if (project.error instanceof GraphQLRequestError && project.error.code === 'NOT_FOUND') return <NotFound what="project" />
  if (project.error) return <p role="alert" className={s.content}>{t('status.loadFailed')}</p>
  if (!project.data) return <p className={s.content}>{t('status.loading')}</p>
  if (project.data.key !== key) {
    const rest = location.pathname.slice(`/p/${key}`.length)
    return <Navigate replace to={`/p/${project.data.key}${rest}${location.search}`} />
  }
  return <Outlet context={{ project: project.data }} />
}

export function NotFound({ what }: { what: 'page' | 'project' | 'issue' }) {
  const { t } = useTranslation()
  const text = t(`notFound.${what}`)
  useTitle(text)
  return (
    <div className={s.notFound}>
      <h1>{text}</h1>
      <Link to="/my-issues">{t('notFound.back')}</Link>
    </div>
  )
}
