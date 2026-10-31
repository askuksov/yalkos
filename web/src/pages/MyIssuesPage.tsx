import { useTranslation } from 'react-i18next'

import { PageHeader } from '../app/PageHeader'

export function MyIssuesPage() {
  const { t } = useTranslation()
  return <PageHeader title={t('nav.myIssues')} />
}
