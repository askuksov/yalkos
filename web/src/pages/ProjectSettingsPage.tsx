import { useTranslation } from 'react-i18next'

import { PageHeader } from '../app/PageHeader'
import { useCurrentProject } from '../app/projectContext'

export function ProjectSettingsPage() {
  const { t } = useTranslation()
  return <PageHeader title={t('nav.projectSettingsTitle', { name: useCurrentProject().name })} />
}
