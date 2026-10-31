import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors } from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { GripVertical } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { CurrentProject } from '../../app/projectContext'
import { errorMessage, request } from '../../api/client'
import type { StateType, UpdateStateInput } from '../../gql/graphql'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { StateIcon } from '../../ui/StateIcon'
import { useToast } from '../../ui/useToast'
import { ColorPicker } from './ColorPicker'
import { CreateStateMutation, DeleteStateMutation, StateIssueCountQuery, UpdateStateMutation } from './operations'
import s from './projects.module.css'
import { useInvalidateProjects } from './useInvalidateProjects'
import { useStateReorder } from './useStateReorder'

type St = CurrentProject['states'][number]
const types: StateType[] = ['BACKLOG', 'UNSTARTED', 'STARTED', 'COMPLETED', 'CANCELED']
const typeKey = (ty: StateType) => ty.toLowerCase() as Lowercase<StateType>

/** States by position: rename, colour, default, drag to reorder, add, delete (moving issues). */
export function StatesEditor({ project, readOnly }: { project: CurrentProject; readOnly: boolean }) {
  const { t } = useTranslation(['projects', 'common'])
  const toast = useToast()
  const invalidate = useInvalidateProjects()
  const states = [...project.states].sort((a, b) => a.position - b.position)
  const { onDragEnd } = useStateReorder(project.id, states.map((x) => x.id))
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))
  const onError = (err: unknown) => toast.show(errorMessage(err), { kind: 'error' })
  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateStateInput }) => request(UpdateStateMutation, { id, input }),
    onError,
    onSettled: () => invalidate(),
  })
  const [deleting, setDeleting] = useState<St | null>(null)
  const [newName, setNewName] = useState('')
  const [newType, setNewType] = useState<StateType>('STARTED')
  const create = useMutation({
    mutationFn: () => request(CreateStateMutation, { projectId: project.id, input: { name: newName.trim(), type: newType, color: '#888888' } }),
    onSuccess: () => setNewName(''),
    onError,
    onSettled: () => invalidate(),
  })
  const add = (e: FormEvent) => {
    e.preventDefault()
    if (newName.trim()) create.mutate()
  }

  return (
    <section className={s.section} aria-labelledby="states-title">
      <h2 id="states-title">{t('states.title')}</h2>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={states.map((x) => x.id)} strategy={verticalListSortingStrategy}>
          <ul className={s.list}>
            {states.map((st) => (
              <StateRow key={st.id} state={st} readOnly={readOnly} onUpdate={(input) => update.mutate({ id: st.id, input })} onDelete={() => setDeleting(st)} />
            ))}
          </ul>
        </SortableContext>
      </DndContext>
      {!readOnly && (
        <form className={s.addRow} onSubmit={add}>
          <input aria-label={t('states.newName')} placeholder={t('states.newName')} maxLength={50} value={newName} onChange={(e) => setNewName(e.target.value)} />
          <select aria-label={t('states.type')} value={newType} onChange={(e) => setNewType(e.target.value as StateType)}>
            {types.map((ty) => (
              <option key={ty} value={ty}>
                {t(`common:stateType.${typeKey(ty)}`)}
              </option>
            ))}
          </select>
          <Button type="submit" size="sm" disabled={!newName.trim() || create.isPending}>
            {t('states.add')}
          </Button>
        </form>
      )}
      <DeleteStateDialog project={project} state={deleting} onClose={() => setDeleting(null)} />
    </section>
  )
}

type RowProps = { state: St; readOnly: boolean; onUpdate: (input: UpdateStateInput) => void; onDelete: () => void }

function StateRow({ state, readOnly, onUpdate, onDelete }: RowProps) {
  const { t } = useTranslation(['projects', 'common'])
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id: state.id, disabled: readOnly })
  const [name, setName] = useState(state.name)
  const canDefault = state.type === 'BACKLOG' || state.type === 'UNSTARTED'
  const rename = () => {
    const next = name.trim()
    if (next && next !== state.name) onUpdate({ name: next })
    else setName(state.name)
  }
  return (
    <li ref={setNodeRef} className={s.stateRow} style={{ transform: transform ? `translate3d(0, ${transform.y}px, 0)` : undefined, transition }}>
      {!readOnly && (
        <button type="button" className={s.handle} aria-label={t('states.drag', { name: state.name })} {...attributes} {...listeners}>
          <GripVertical size={14} aria-hidden />
        </button>
      )}
      <StateIcon type={state.type} color={state.color} />
      <input
        type="text"
        aria-label={t('states.name', { name: state.name })}
        value={name}
        maxLength={50}
        readOnly={readOnly}
        onChange={(e) => setName(e.target.value)}
        onBlur={rename}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur()
          if (e.key === 'Escape') setName(state.name)
        }}
      />
      <span className={s.typeLabel}>{t(`common:stateType.${typeKey(state.type)}`)}</span>
      <ColorPicker value={state.color} onChange={(color) => onUpdate({ color })} label={t('states.color', { name: state.name })} disabled={readOnly} />
      <label className={s.muted} title={t('states.defaultOf', { name: state.name })}>
        <input
          type="radio"
          name="default-state"
          aria-label={t('states.defaultOf', { name: state.name })}
          checked={state.isDefault}
          disabled={readOnly || !canDefault}
          onChange={() => onUpdate({ isDefault: true })}
        />{' '}
        {t('states.default')}
      </label>
      {!readOnly && (
        <Button size="sm" variant="ghost" aria-label={t('states.deleteOf', { name: state.name })} disabled={state.isDefault} onClick={onDelete}>
          {t('states.delete')}
        </Button>
      )}
    </li>
  )
}

/** Confirms a delete; a state with issues needs a state to move them to. */
function DeleteStateDialog({ project, state, onClose }: { project: CurrentProject; state: St | null; onClose: () => void }) {
  const { t } = useTranslation(['projects', 'common'])
  return (
    <Dialog open={!!state} onClose={onClose} title={t('states.deleteTitle')}>
      {state && <DeleteStateForm project={project} state={state} onClose={onClose} />}
    </Dialog>
  )
}

function DeleteStateForm({ project, state, onClose }: { project: CurrentProject; state: St; onClose: () => void }) {
  const { t } = useTranslation(['projects', 'common'])
  const qc = useQueryClient()
  const toast = useToast()
  const invalidate = useInvalidateProjects()
  const [target, setTarget] = useState('')
  const [count, setCount] = useState<number | null>(null)
  const [asked, setAsked] = useState(false)
  if (!asked) {
    setAsked(true)
    void qc
      .fetchQuery({
        queryKey: ['stateIssueCount', state.id],
        queryFn: () => request(StateIssueCountQuery, { filter: { project: { eq: project.id }, state: { eq: state.id }, includeArchived: true } }),
        staleTime: 0,
      })
      .then((d) => setCount(d.issues.totalCount))
      .catch(() => setCount(0))
  }
  const remove = useMutation({
    mutationFn: () => request(DeleteStateMutation, { id: state.id, moveIssuesTo: count ? target : null }),
    onSuccess: onClose,
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => invalidate(),
  })
  const others = project.states.filter((x) => x.id !== state.id).sort((a, b) => a.position - b.position)
  return (
    <>
      {count === null ? (
        <p>{t('common:status.loading')}</p>
      ) : count > 0 ? (
        <label>
          {t('states.deleteUsed', { count, name: state.name })}
          <select aria-label={t('states.moveTo')} value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value="">{t('states.chooseState')}</option>
            {others.map((x) => (
              <option key={x.id} value={x.id}>
                {x.name}
              </option>
            ))}
          </select>
        </label>
      ) : (
        <p>{t('states.deleteBody', { name: state.name })}</p>
      )}
      <div className={s.actions}>
        <Button onClick={onClose}>{t('common:actions.cancel')}</Button>
        <Button variant="danger" disabled={count === null || (count > 0 && !target) || remove.isPending} onClick={() => remove.mutate()}>
          {t('states.delete')}
        </Button>
      </div>
    </>
  )
}
