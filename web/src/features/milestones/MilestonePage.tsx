import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MoreHorizontal } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router'

import { PageHeader } from '../../app/PageHeader'
import { NotFound } from '../../app/ProjectRoute'
import { useCurrentProject } from '../../app/projectContext'
import { errorMessage, GraphQLRequestError, request } from '../../api/client'
import { useIsAdmin, useMembers, useViewer } from '../../api/hooks'
import type { MilestonePageQuery as MilestonePageData, MilestoneStatus as Status, UpdateMilestoneInput } from '../../gql/graphql'
import { compareNames } from '../../i18n'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { Menu } from '../../ui/Menu'
import { useToast } from '../../ui/useToast'
import { Markdown } from '../issue-page/Markdown'
import { Board } from '../board/Board'
import { useCreateDefaults } from '../issues/createIssue'
import { FilterBar, FilterChips } from '../issues/FilterBar'
import is from '../issues/issues.module.css'
import { IssueList } from '../issues/IssueList'
import { useViewParams } from '../issues/useViewParams'
import { useLabels } from '../labels/useLabels'
import { MarkdownEditor } from '../markdown/MarkdownEditor'
import { MilestoneProgress } from './MilestoneProgress'
import s from './milestones.module.css'
import { statuses, statusKey } from './status'
import { DeleteMilestoneMutation, MilestonePageQuery, UpdateMilestoneMutation } from './operations'

/** One milestone: editable header (name, status, target date, lead, description) and its issues. */
export function MilestonePage() {
  const { t } = useTranslation()
  const { id = '' } = useParams()
  const q = useQuery({ queryKey: ['milestone', id], queryFn: () => request(MilestonePageQuery, { id }) })
  if (q.error instanceof GraphQLRequestError && (q.error.code === 'NOT_FOUND' || q.error.code === 'INVALID_INPUT')) return <NotFound what="page" />
  if (q.error) return <p role="alert">{t('status.loadFailed')}</p>
  if (!q.data) return <p>{t('status.loading')}</p>
  return <MilestoneView key={q.data.milestone.id} milestone={q.data.milestone} />
}

type View = MilestonePageData['milestone']

function MilestoneView({ milestone: m }: { milestone: View }) {
  const { t } = useTranslation(['issues', 'common'])
  const qc = useQueryClient()
  const toast = useToast()
  const navigate = useNavigate()
  const project = useCurrentProject()
  const viewer = useViewer().data
  const admin = useIsAdmin()
  const users = useMembers().data ?? []
  const labels = useLabels().data
  const readOnly = !!project.archivedAt
  const [params, setParams] = useViewParams('list')
  const [view, setView] = useState<'list' | 'board'>('list')
  const [name, setName] = useState(m.name)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [menuAnchor, setMenuAnchor] = useState<HTMLButtonElement | null>(null)
  const [menu, setMenu] = useState(false)
  const [deleting, setDeleting] = useState(false)
  useCreateDefaults(readOnly ? null : { projectId: project.id, milestoneId: m.id })

  const update = useMutation({
    mutationFn: (input: UpdateMilestoneInput) => request(UpdateMilestoneMutation, { id: m.id, input }),
    onSuccess: () => setEditing(false),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => Promise.all(['milestone', 'milestones'].map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  })
  const remove = useMutation({
    mutationFn: () => request(DeleteMilestoneMutation, { id: m.id }),
    onSuccess: () => {
      void Promise.all(['milestones', 'issues', 'issue'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
      navigate(`/p/${project.key}/milestones`, { replace: true })
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })
  const canDelete = !readOnly && (admin || m.createdBy?.id === viewer?.id)
  const commitName = () => {
    const next = name.trim()
    if (next && next !== m.name) update.mutate({ name: next })
    else setName(m.name)
  }
  const scope = { params, onChange: setParams, states: project.states, users, viewerId: viewer?.id ?? '', labels }

  return (
    <>
      <PageHeader title={m.name}>
        <FilterBar {...scope} />
        <div role="group" aria-label={t('milestones.view')}>
          {(['list', 'board'] as const).map((v) => (
            <Button key={v} size="sm" variant={view === v ? 'primary' : 'ghost'} aria-pressed={view === v} onClick={() => setView(v)}>
              {t(`milestones.${v}`)}
            </Button>
          ))}
        </div>
        {canDelete && (
          <Button ref={setMenuAnchor} size="sm" variant="ghost" icon={<MoreHorizontal size={16} aria-hidden />} aria-label={t('milestones.more')} aria-haspopup="menu" onClick={() => setMenu(!menu)} />
        )}
      </PageHeader>
      <Menu anchor={menuAnchor} open={menu} onClose={() => setMenu(false)} label={t('milestones.more')} items={[{ label: t('milestones.delete'), danger: true, onSelect: () => setDeleting(true) }]} />
      <section className={s.header} aria-label={t('milestones.details')}>
        <input
          className={s.nameInput}
          aria-label={t('milestones.name')}
          disabled={readOnly}
          maxLength={100}
          value={name}
          onChange={(e) => setName(e.target.value)}
          onBlur={commitName}
          onKeyDown={(e) => {
            if (e.key === 'Enter') e.currentTarget.blur()
          }}
        />
        <div className={s.fields}>
          <label>
            {t('milestones.statusLabel')}
            <select
              aria-label={t('milestones.statusLabel')}
              disabled={readOnly}
              value={m.statusAutomatic ? 'auto' : m.status}
              onChange={(e) => update.mutate(e.target.value === 'auto' ? { statusAutomatic: true } : { status: e.target.value as Status })}
            >
              <option value="auto">
                {m.statusAutomatic ? t('milestones.automaticIs', { status: t(`milestones.status.${statusKey(m.status)}`) }) : t('milestones.automatic')}
              </option>
              {statuses.map((st) => (
                <option key={st} value={st}>
                  {t(`milestones.status.${statusKey(st)}`)}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t('milestones.targetDate')}
            <input
              type="date"
              key={m.targetDate ?? ''}
              disabled={readOnly}
              defaultValue={m.targetDate ?? ''}
              data-overdue={m.overdue || undefined}
              className={s.date}
              onBlur={(e) => {
                const next = e.target.value || null
                if (next !== (m.targetDate ?? null)) update.mutate({ targetDate: next })
              }}
            />
          </label>
          <label>
            {t('milestones.lead')}
            <select disabled={readOnly} value={m.lead?.id ?? ''} onChange={(e) => update.mutate({ leadId: e.target.value || null })}>
              <option value="">{t('milestones.noLead')}</option>
              {users
                .filter((u) => !u.disabled || u.id === m.lead?.id)
                .sort((a, b) => compareNames(a.name, b.name))
                .map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name}
                  </option>
                ))}
            </select>
          </label>
          <MilestoneProgress completed={m.progress.completed} total={m.progress.total} />
        </div>
        {editing ? (
          <div className={s.form}>
            <MarkdownEditor label={t('milestones.description')} autoFocus rows={6} value={draft} onChange={setDraft} />
            <div className={s.actions}>
              <Button size="sm" onClick={() => setEditing(false)}>
                {t('common:actions.cancel')}
              </Button>
              <Button size="sm" variant="primary" disabled={update.isPending} onClick={() => update.mutate({ description: draft })}>
                {t('common:actions.save')}
              </Button>
            </div>
          </div>
        ) : (
          <div>
            {m.description.trim() ? <Markdown source={m.description} /> : <p className={is.emptyText}>{t('milestones.noDescription')}</p>}
            {!readOnly && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setDraft(m.description)
                  setEditing(true)
                }}
              >
                {t('milestones.editDescription')}
              </Button>
            )}
          </div>
        )}
      </section>
      <div className={is.chips}>
        <span className={is.chip}>
          <span className={is.chipText}>{t('filter.chip', { name: t('milestones.milestone'), values: m.name })}</span>
        </span>
      </div>
      <FilterChips {...scope} />
      {view === 'list' ? (
        <IssueList
          scope={{ kind: 'project', projectId: project.id, projectKey: project.key, states: project.states, archived: readOnly, milestoneId: m.id }}
          params={params}
          users={users}
          viewerId={viewer?.id ?? ''}
        />
      ) : (
        <Board project={{ id: project.id, key: project.key, states: project.states, archived: readOnly, milestoneId: m.id }} params={params} viewerId={viewer?.id ?? ''} />
      )}
      <Dialog open={deleting} onClose={() => setDeleting(false)} title={t('milestones.deleteTitle', { name: m.name })}>
        <p>{t('milestones.deleteBody')}</p>
        <div className={s.actions}>
          <Button onClick={() => setDeleting(false)}>{t('common:actions.cancel')}</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('milestones.delete')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
