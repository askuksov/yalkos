import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors } from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { GripVertical, Plus } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router'

import as from '../../app/app.module.css'
import { PageHeader } from '../../app/PageHeader'
import { useCurrentProject } from '../../app/projectContext'
import { errorMessage, request } from '../../api/client'
import { useMembers } from '../../api/hooks'
import type { MilestoneFieldsFragment } from '../../gql/graphql'
import { compareNames } from '../../i18n'
import { Avatar } from '../../ui/Avatar'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { MarkdownEditor } from '../markdown/MarkdownEditor'
import { MilestoneProgress } from './MilestoneProgress'
import s from './milestones.module.css'
import { MilestoneStatus } from './MilestoneStatus'
import { CreateMilestoneMutation } from './operations'
import { useMilestoneReorder } from './useMilestoneReorder'
import { useMilestones } from './useMilestones'

/** A project's milestones in manual order (drag to reorder) and "New milestone". */
export function MilestonesPage() {
  const { t } = useTranslation('issues')
  const project = useCurrentProject()
  const archived = !!project.archivedAt
  const list = useMilestones(project.key).data ?? []
  const { onDragEnd } = useMilestoneReorder(project.id, project.key, list.map((m) => m.id))
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))
  // "Create milestone" in the palette opens this page with ?new=1.
  const [sp, setSp] = useSearchParams()
  const [creating, setCreatingState] = useState(() => sp.get('new') === '1' && !archived)
  const setCreating = (v: boolean) => {
    setCreatingState(v)
    if (!v && sp.has('new')) setSp({}, { replace: true })
  }
  return (
    <>
      <PageHeader title={t('milestones.title', { name: project.name })}>
        {!archived && (
          <Button size="sm" variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setCreating(true)}>
            {t('milestones.new')}
          </Button>
        )}
      </PageHeader>
      <div className={as.content}>
        {list.length === 0 && <p>{t('milestones.empty')}</p>}
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext items={list.map((m) => m.id)} strategy={verticalListSortingStrategy}>
            <ul className={s.list}>
              {list.map((m) => (
                <MilestoneRow key={m.id} milestone={m} projectKey={project.key} draggable={!archived} />
              ))}
            </ul>
          </SortableContext>
        </DndContext>
      </div>
      <Dialog open={creating} onClose={() => setCreating(false)} title={t('milestones.new')} wide>
        {creating && <NewMilestone projectId={project.id} projectKey={project.key} onClose={() => setCreating(false)} />}
      </Dialog>
    </>
  )
}

function MilestoneRow({ milestone: m, projectKey, draggable }: { milestone: MilestoneFieldsFragment; projectKey: string; draggable: boolean }) {
  const { t } = useTranslation('issues')
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id: m.id, disabled: !draggable })
  const style = { transform: transform ? `translate3d(0, ${transform.y}px, 0)` : undefined, transition }
  return (
    <li ref={setNodeRef} style={style} className={s.row}>
      {draggable && (
        <button type="button" className={s.handle} aria-label={t('milestones.drag', { name: m.name })} {...attributes} {...listeners}>
          <GripVertical size={14} aria-hidden />
        </button>
      )}
      <MilestoneStatus status={m.status} />
      <Link to={`/p/${projectKey}/milestones/${m.id}`} className={s.name}>
        {m.name}
      </Link>
      <MilestoneProgress completed={m.progress.completed} total={m.progress.total} />
      {m.targetDate && (
        <span className={s.date} data-overdue={m.overdue || undefined} title={t(m.overdue ? 'milestones.overdue' : 'milestones.target', { date: m.targetDate })}>
          {m.targetDate}
        </span>
      )}
      {m.lead && <Avatar id={m.lead.id} name={m.lead.name} size={18} />}
    </li>
  )
}

/** Name, target date, lead and description of a new milestone. */
export function NewMilestone({ projectId, projectKey, onClose }: { projectId: string; projectKey: string; onClose: () => void }) {
  const { t } = useTranslation(['issues', 'common'])
  const qc = useQueryClient()
  const navigate = useNavigate()
  const users = useMembers().data ?? []
  const [name, setName] = useState('')
  const [date, setDate] = useState('')
  const [lead, setLead] = useState('')
  const [description, setDescription] = useState('')
  const [uploading, setUploading] = useState(false)
  const create = useMutation({
    mutationFn: () =>
      request(CreateMilestoneMutation, {
        projectId,
        input: { name: name.trim(), ...(date ? { targetDate: date } : {}), ...(lead ? { leadId: lead } : {}), ...(description.trim() ? { description } : {}) },
      }),
    onSuccess: (d) => {
      void qc.invalidateQueries({ queryKey: ['milestones'] })
      onClose()
      navigate(`/p/${projectKey}/milestones/${d.createMilestone.id}`)
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (name.trim() && !uploading) create.mutate()
  }
  return (
    <form className={s.form} onSubmit={submit}>
      <input aria-label={t('milestones.name')} placeholder={t('milestones.name')} autoFocus required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
      <div className={s.fields}>
        <label>
          {t('milestones.targetDate')}
          <input type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        </label>
        <label>
          {t('milestones.lead')}
          <select value={lead} onChange={(e) => setLead(e.target.value)}>
            <option value="">{t('milestones.noLead')}</option>
            {users
              .filter((u) => !u.disabled)
              .sort((a, b) => compareNames(a.name, b.name))
              .map((u) => (
                <option key={u.id} value={u.id}>
                  {u.name}
                </option>
              ))}
          </select>
        </label>
      </div>
      <MarkdownEditor label={t('milestones.description')} rows={5} value={description} onChange={setDescription} onBusyChange={setUploading} />
      {create.error && <p role="alert">{errorMessage(create.error)}</p>}
      <div className={s.actions}>
        <Button onClick={onClose}>{t('common:actions.cancel')}</Button>
        <Button type="submit" variant="primary" disabled={!name.trim() || create.isPending || uploading}>
          {t('milestones.create')}
        </Button>
      </div>
    </form>
  )
}
