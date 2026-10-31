import { useTranslation } from 'react-i18next'

import { PageHeader } from '../app/PageHeader'
import { useMembers, useViewer } from '../api/hooks'
import { DisplayMenu } from '../features/issues/DisplayMenu'
import { FilterBar, FilterChips } from '../features/issues/FilterBar'
import { useLabels } from '../features/labels/useLabels'
import { IssueList } from '../features/issues/IssueList'
import { useViewParams } from '../features/issues/useViewParams'

export function MyIssuesPage() {
  const { t } = useTranslation()
  const [params, setParams] = useViewParams('list')
  const viewer = useViewer().data
  const users = useMembers().data ?? []
  const labels = useLabels().data
  const scope = { params, onChange: setParams, users, viewerId: viewer?.id ?? '', hideAssignee: true, labels }
  return (
    <>
      <PageHeader title={t('nav.myIssues')}>
        <FilterBar {...scope} />
        <DisplayMenu params={params} onChange={setParams} nestable={false} />
      </PageHeader>
      <FilterChips {...scope} />
      <IssueList scope={{ kind: 'my' }} params={params} users={users} viewerId={viewer?.id ?? ''} />
    </>
  )
}
