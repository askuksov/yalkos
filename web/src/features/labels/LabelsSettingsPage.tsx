import { closestCenter, DndContext, type DragEndEvent, KeyboardSensor, PointerSensor, useSensor, useSensors } from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GripVertical, Trash2 } from 'lucide-react'
import { type FormEvent, type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'

import as from '../../app/app.module.css'
import { PageHeader } from '../../app/PageHeader'
import { errorMessage, GraphQLRequestError, request } from '../../api/client'
import { useIsAdmin } from '../../api/hooks'
import type { UpdateLabelInput } from '../../gql/graphql'
import { formatNumber } from '../../i18n'
import { dropOrder } from '../../lib/sortOrder'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { ColorPicker } from '../projects/ColorPicker'
import s from './labels.module.css'
import { CreateLabelMutation, DeleteLabelMutation, LabelIssueCountQuery, SetDefaultTypeMutation, UpdateLabelMutation } from './operations'
import { colorFor, type LabelCatalog, type LabelItem, useLabels } from './useLabels'

/** Text for a failed label change; a regroup conflict names how many issues. */
function labelError(err: unknown, t: (k: 'labelsSettings.conflict', o: { count: number }) => string): string {
  if (err instanceof GraphQLRequestError && err.reason === 'not_allowed' && typeof err.params?.conflicts === 'number') {
    return t('labelsSettings.conflict', { count: err.params.conflicts })
  }
  return errorMessage(err)
}

/**
 * Workspace labels: groups with their labels, then free labels. Owner/Admin
 * rename, recolour, regroup (drag within a list, or the group select),
 * toggle exclusivity, pick the default type and delete; members read.
 */
export function LabelsSettingsPage() {
  const { t } = useTranslation(['issues', 'common'])
  const qc = useQueryClient()
  const admin = useIsAdmin()
  const labels = useLabels().data
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<LabelItem | null>(null)
  const refresh = () => Promise.all(['labels', 'issues', 'children', 'issue'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
  const onError = (err: unknown) => setError(labelError(err, t))
  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateLabelInput }) => request(UpdateLabelMutation, { id, input }),
    onMutate: () => setError(null),
    onError,
    onSettled: refresh,
  })
  const setDefault = useMutation({
    mutationFn: (labelId: string | null) => request(SetDefaultTypeMutation, { labelId }),
    onError,
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: string) => request(DeleteLabelMutation, { id }),
    onSuccess: () => setDeleting(null),
    onError,
    onSettled: refresh,
  })
  if (!labels) return <PageHeader title={t('labelsSettings.title')} />
  const groups = labels.top.filter((l) => l.isGroup)
  const free = labels.top.filter((l) => !l.isGroup)
  const rowProps = { labels, admin, groups, onUpdate: (id: string, input: UpdateLabelInput) => update.mutate({ id, input }), onDelete: setDeleting }
  return (
    <>
      <PageHeader title={t('labelsSettings.title')} />
      <div className={[as.content, s.page].join(' ')}>
        {error && <p role="alert">{error}</p>}
        {groups.map((g) => (
          <section key={g.id} aria-label={g.name}>
            <div className={s.row}>
              <LabelBasics label={g} {...rowProps} />
              {admin && (
                <label>
                  <input
                    type="checkbox"
                    checked={g.exclusive}
                    disabled={g.id === labels.typeGroupId}
                    onChange={(e) => update.mutate({ id: g.id, input: { exclusive: e.target.checked } })}
                  />{' '}
                  {t('labelsSettings.exclusive')}
                </label>
              )}
              {admin && g.id !== labels.typeGroupId && <DeleteButton label={g} onDelete={setDeleting} />}
            </div>
            <SortableList
              items={g.children}
              admin={admin}
              onMove={(id, position) => update.mutate({ id, input: { position } })}
              render={(l) => (
                <LabelRow label={l} {...rowProps}>
                  {g.id === labels.typeGroupId && admin && (
                    <input
                      type="radio"
                      name="default-type"
                      aria-label={t('labelsSettings.defaultType', { name: l.name })}
                      checked={labels.defaultTypeId === l.id}
                      onChange={() => setDefault.mutate(l.id)}
                    />
                  )}
                  {g.id === labels.typeGroupId && !admin && labels.defaultTypeId === l.id && <span className={s.count}>{t('labelsSettings.default')}</span>}
                </LabelRow>
              )}
            />
          </section>
        ))}
        <section aria-label={t('labelsSettings.free')}>
          <h2>{t('labelsSettings.free')}</h2>
          <SortableList items={free} admin={admin} onMove={(id, position) => update.mutate({ id, input: { position } })} render={(l) => <LabelRow label={l} {...rowProps} />} />
        </section>
        <NewLabel labels={labels} admin={admin} onError={onError} onDone={refresh} />
      </div>
      <Dialog open={!!deleting} onClose={() => setDeleting(null)} title={t('labelsSettings.deleteTitle', { name: deleting?.name ?? '' })}>
        <p>{t(deleting?.isGroup ? 'labelsSettings.deleteGroupBody' : 'labelsSettings.deleteBody')}</p>
        <div className={s.addRow}>
          <Button onClick={() => setDeleting(null)}>{t('common:actions.cancel')}</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => deleting && remove.mutate(deleting.id)}>
            {t('labelsSettings.delete')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}

type RowProps = {
  label: LabelItem
  labels: LabelCatalog
  admin: boolean
  groups: LabelItem[]
  onUpdate: (id: string, input: UpdateLabelInput) => void
  onDelete: (l: LabelItem) => void
}

/** Colour and name: editable for Owner/Admin, text for members. */
function LabelBasics({ label, admin, onUpdate }: RowProps) {
  const { t } = useTranslation('issues')
  const [name, setName] = useState(label.name)
  const commit = () => {
    const next = name.trim()
    if (next && next !== label.name) onUpdate(label.id, { name: next })
    else setName(label.name)
  }
  if (!admin) {
    return (
      <>
        <span className={s.dot} style={{ background: label.color }} aria-hidden />
        <span className={s.grow}>{label.name}</span>
      </>
    )
  }
  return (
    <>
      <ColorPicker value={label.color} label={t('labelsSettings.colorOf', { name: label.name })} onChange={(color) => onUpdate(label.id, { color })} />
      <input
        type="text"
        aria-label={t('labelsSettings.nameOf', { name: label.name })}
        maxLength={50}
        value={name}
        onChange={(e) => setName(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur()
        }}
      />
    </>
  )
}

function DeleteButton({ label, onDelete }: { label: LabelItem; onDelete: (l: LabelItem) => void }) {
  const { t } = useTranslation('issues')
  return (
    <button type="button" className={s.handle} aria-label={t('labelsSettings.deleteOf', { name: label.name })} onClick={() => onDelete(label)}>
      <Trash2 size={14} aria-hidden />
    </button>
  )
}

/** One label: basics, group select, issue count, delete. */
function LabelRow(props: RowProps & { children?: ReactNode }) {
  const { t } = useTranslation('issues')
  const { label, labels, admin, groups, onUpdate, onDelete, children } = props
  const count = useQuery({
    queryKey: ['labelIssueCount', label.id],
    queryFn: () => request(LabelIssueCountQuery, { id: label.id }),
    select: (d) => d.issues.totalCount,
  }).data
  // Type labels stay in the type group; other labels move between free and non-type groups.
  const movable = admin && label.parent?.id !== labels.typeGroupId
  return (
    <>
      <LabelBasics {...props} />
      {children}
      {movable && (
        <select
          aria-label={t('labelsSettings.groupOf', { name: label.name })}
          value={label.parent?.id ?? ''}
          onChange={(e) => onUpdate(label.id, { parentId: e.target.value || null })}
        >
          <option value="">{t('labelsSettings.noGroup')}</option>
          {groups
            .filter((g) => g.id !== labels.typeGroupId)
            .map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
        </select>
      )}
      <span className={s.count} title={t('labelsSettings.issues', { count: count ?? 0 })}>
        {count === undefined ? '' : formatNumber(count)}
      </span>
      {admin && <DeleteButton label={label} onDelete={onDelete} />}
    </>
  )
}

/** A list of label rows; Owner/Admin drag to reorder (keyboard: Space, arrows, Space). */
function SortableList({ items, admin, onMove, render }: { items: LabelItem[]; admin: boolean; onMove: (id: string, position: number) => void; render: (l: LabelItem) => ReactNode }) {
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))
  const ids = items.map((l) => l.id)
  const onDragEnd = (e: DragEndEvent) => {
    const from = ids.indexOf(String(e.active.id))
    const to = e.over ? ids.indexOf(String(e.over.id)) : -1
    if (from < 0 || to < 0 || from === to) return
    onMove(ids[from], dropOrder(items.map((l) => l.position), to, from))
  }
  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <SortableContext items={ids} strategy={verticalListSortingStrategy}>
        <ul className={s.list}>
          {items.map((l) => (
            <SortableRow key={l.id} id={l.id} admin={admin}>
              {render(l)}
            </SortableRow>
          ))}
        </ul>
      </SortableContext>
    </DndContext>
  )
}

function SortableRow({ id, admin, children }: { id: string; admin: boolean; children: ReactNode }) {
  const { t } = useTranslation('issues')
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id, disabled: !admin })
  const style = { transform: transform ? `translate3d(0, ${transform.y}px, 0)` : undefined, transition }
  return (
    <li ref={setNodeRef} style={style} className={[s.row, s.child].join(' ')}>
      {admin && (
        <button type="button" className={s.handle} aria-label={t('labelsSettings.drag')} {...attributes} {...listeners}>
          <GripVertical size={14} aria-hidden />
        </button>
      )}
      {children}
    </li>
  )
}

/** New label (members: free; Owner/Admin: also in a group) and new group (Owner/Admin). */
function NewLabel({ labels, admin, onError, onDone }: { labels: LabelCatalog; admin: boolean; onError: (e: unknown) => void; onDone: () => void }) {
  const { t } = useTranslation('issues')
  const [name, setName] = useState('')
  const [parent, setParent] = useState('')
  const create = useMutation({
    mutationFn: (v: { name: string; parentId?: string; isGroup?: boolean }) => request(CreateLabelMutation, { input: { ...v, color: colorFor(v.name) } }),
    onSuccess: () => {
      setName('')
      onDone()
    },
    onError,
  })
  const submit = (isGroup: boolean) => (e?: FormEvent) => {
    e?.preventDefault()
    const n = name.trim()
    if (!n) return
    create.mutate(isGroup ? { name: n, isGroup: true } : { name: n, ...(parent ? { parentId: parent } : {}) })
  }
  const groups = labels.top.filter((l) => l.isGroup && (admin || l.id !== labels.typeGroupId))
  return (
    <form className={s.addRow} onSubmit={submit(false)}>
      <input aria-label={t('labelsSettings.newName')} placeholder={t('labelsSettings.newName')} maxLength={50} value={name} onChange={(e) => setName(e.target.value)} />
      <select aria-label={t('labelsSettings.newGroup')} value={parent} onChange={(e) => setParent(e.target.value)}>
        <option value="">{t('labelsSettings.noGroup')}</option>
        {groups.map((g) => (
          <option key={g.id} value={g.id}>
            {g.name}
          </option>
        ))}
      </select>
      <Button type="submit" size="sm" disabled={!name.trim() || create.isPending}>
        {t('labelsSettings.addLabel')}
      </Button>
      {admin && (
        <Button size="sm" disabled={!name.trim() || create.isPending} onClick={() => submit(true)()}>
          {t('labelsSettings.addGroup')}
        </Button>
      )}
    </form>
  )
}
