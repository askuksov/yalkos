import { useNavigate } from 'react-router'

import { PageHeader } from '../app/PageHeader'
import { useCurrentProject } from '../app/projectContext'
import { useMembers, useViewer } from '../api/hooks'
import { Board } from '../features/board/Board'
import { useCreateDefaults } from '../features/issues/createIssue'
import { DisplayMenu } from '../features/issues/DisplayMenu'
import { FilterBar, FilterChips } from '../features/issues/FilterBar'
import { useLabels } from '../features/labels/useLabels'
import { useMilestones } from '../features/milestones/useMilestones'
import { toSearch } from '../features/issues/filters'
import { useViewParams } from '../features/issues/useViewParams'
import { ArchivedBanner } from './ProjectIssuesPage'

export function ProjectBoardPage() {
  const project = useCurrentProject()
  const navigate = useNavigate()
  const [params, setParams] = useViewParams('board')
  const viewer = useViewer().data
  const users = useMembers().data ?? []
  const labels = useLabels().data
  const milestones = useMilestones(project.key).data
  const archived = !!project.archivedAt
  useCreateDefaults(archived ? null : { projectId: project.id })
  const scope = { params, onChange: setParams, states: project.states, users, viewerId: viewer?.id ?? '', labels, milestones }
  const onView = (view: 'list' | 'board') => {
    const q = toSearch(params)
    navigate(`/p/${project.key}${view === 'board' ? '/board' : ''}${q ? `?${q}` : ''}`)
  }
  return (
    <>
      <PageHeader title={project.name}>
        <FilterBar {...scope} />
        <DisplayMenu params={params} onChange={setParams} onView={onView} />
      </PageHeader>
      {archived && <ArchivedBanner projectId={project.id} />}
      <FilterChips {...scope} />
      <Board project={{ id: project.id, key: project.key, states: project.states, archived }} params={params} viewerId={viewer?.id ?? ''} />
    </>
  )
}
