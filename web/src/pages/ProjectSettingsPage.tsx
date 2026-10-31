import { useTranslation } from 'react-i18next'

import { PageHeader } from '../app/PageHeader'
import { useCurrentProject } from '../app/projectContext'
import { useIsAdmin } from '../api/hooks'
import { DangerZone } from '../features/projects/DangerZone'
import { GeneralSettings } from '../features/projects/GeneralSettings'
import { ProjectMembers } from '../features/projects/ProjectMembers'
import s from '../features/projects/projects.module.css'
import { StatesEditor } from '../features/projects/StatesEditor'

/** General, states, members, danger zone; members see everything read-only. */
export function ProjectSettingsPage() {
  const { t } = useTranslation('projects')
  const project = useCurrentProject()
  const isAdmin = useIsAdmin()
  const readOnly = !isAdmin
  return (
    <>
      <PageHeader title={t('settings.title', { name: project.name })} />
      <div className={s.content}>
        {readOnly && <p className={s.muted}>{t('settings.readOnly')}</p>}
        <GeneralSettings key={`${project.id}-${project.key}`} project={project} readOnly={readOnly} />
        <StatesEditor project={project} readOnly={readOnly} />
        <ProjectMembers project={project} readOnly={readOnly} />
        {isAdmin && <DangerZone project={project} />}
      </div>
    </>
  )
}
