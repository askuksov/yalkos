import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { PageHeader } from '../app/PageHeader'
import { useCurrentProject } from '../app/projectContext'
import { errorMessage, request } from '../api/client'
import { useIsAdmin, useMembers, useViewer } from '../api/hooks'
import { DisplayMenu } from '../features/issues/DisplayMenu'
import { FilterBar, FilterChips } from '../features/issues/FilterBar'
import { useLabels } from '../features/labels/useLabels'
import { useMilestones } from '../features/milestones/useMilestones'
import { useCreateDefaults, useCreateIssue } from '../features/issues/createIssue'
import { UnarchiveProjectMutation } from '../features/issues/fragments'
import { IssueList } from '../features/issues/IssueList'
import s from '../features/issues/issues.module.css'
import { toSearch } from '../features/issues/filters'
import { useViewParams } from '../features/issues/useViewParams'
import { Button } from '../ui/Button'
import { useToast } from '../ui/useToast'

export function ProjectIssuesPage() {
  const { t } = useTranslation('issues')
  const project = useCurrentProject()
  const [params, setParams] = useViewParams('list')
  const viewer = useViewer().data
  const users = useMembers().data ?? []
  const labels = useLabels().data
  const milestones = useMilestones(project.key).data
  const archived = !!project.archivedAt
  const scope = { params, onChange: setParams, states: project.states, users, viewerId: viewer?.id ?? '', labels, milestones }
  const { openCreateIssue } = useCreateIssue()
  const navigate = useNavigate()
  const onView = (view: 'list' | 'board') => {
    const q = toSearch(params)
    navigate(`/p/${project.key}${view === 'board' ? '/board' : ''}${q ? `?${q}` : ''}`)
  }
  useCreateDefaults(archived ? null : { projectId: project.id })
  return (
    <>
      <PageHeader title={project.name}>
        <FilterBar {...scope} />
        <DisplayMenu params={params} onChange={setParams} onView={onView} />
      </PageHeader>
      {archived && <ArchivedBanner projectId={project.id} />}
      <FilterChips {...scope} />
      <IssueList
        scope={{ kind: 'project', projectId: project.id, projectKey: project.key, states: project.states, archived }}
        params={params}
        users={users}
        viewerId={viewer?.id ?? ''}
        empty={
          <div className={s.empty}>
            <p>{t('list.empty')}</p>
            {!archived && (
              <Button variant="primary" onClick={() => openCreateIssue({ projectId: project.id })}>
                {t('create.emptyButton')}
              </Button>
            )}
          </div>
        }
        groupAction={(stateId) => (
          <Button
            size="sm"
            variant="ghost"
            icon={<Plus size={14} aria-hidden />}
            aria-label={t('create.inState', { state: project.states.find((x) => x.id === stateId)?.name ?? '' })}
            onClick={() => openCreateIssue({ projectId: project.id, stateId })}
          />
        )}
      />
    </>
  )
}

export function ArchivedBanner({ projectId }: { projectId: string }) {
  const { t } = useTranslation('issues')
  const isAdmin = useIsAdmin()
  const qc = useQueryClient()
  const toast = useToast()
  const unarchive = useMutation({
    mutationFn: () => request(UnarchiveProjectMutation, { id: projectId }),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: ['project'] }), qc.invalidateQueries({ queryKey: ['projects'] })]),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })
  return (
    <div className={s.banner} role="status">
      <span>{t('list.archived')}</span>
      {isAdmin && (
        <Button size="sm" disabled={unarchive.isPending} onClick={() => unarchive.mutate()}>
          {t('list.unarchive')}
        </Button>
      )}
    </div>
  )
}
