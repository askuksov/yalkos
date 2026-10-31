import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors } from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useQuery } from '@tanstack/react-query'
import { GripVertical, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { request } from '../../api/client'
import type { IssueRowFieldsFragment } from '../../gql/graphql'
import { Avatar } from '../../ui/Avatar'
import { Button } from '../../ui/Button'
import { StateIcon } from '../../ui/StateIcon'
import { useCreateIssue } from '../issues/createIssue'
import { IssueChildrenQuery } from '../issues/fragments'
import s from './issue-page.module.css'
import type { PageIssue } from './types'
import { childOrder, childrenKey, useChildReorder } from './useChildReorder'

/** Progress, the direct children in manual order (drag to reorder) and "+ Add". */
export function SubIssues({ issue, readOnly }: { issue: PageIssue; readOnly: boolean }) {
  const { t } = useTranslation('issue')
  const { openCreateIssue } = useCreateIssue()
  const { onDragEnd } = useChildReorder(issue.id)
  const children = useQuery({ queryKey: childrenKey(issue.id), queryFn: () => request(IssueChildrenQuery, { id: issue.id, orderBy: childOrder }) })
  const nodes = children.data?.issue.children.nodes ?? []
  const { completed, total } = issue.subIssueProgress
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))

  if (!nodes.length && readOnly) return null
  return (
    <section className={s.subIssues} aria-labelledby="sub-issues-title">
      <div className={s.sectionHeader}>
        <h2 id="sub-issues-title" className={s.sectionTitle}>
          {t('subIssues.title')}
        </h2>
        {total > 0 && (
          <>
            <span className={s.muted} title={t('subIssues.progress', { completed, total })}>
              {completed}/{total}
            </span>
            <progress className={s.bar} max={total} value={completed} aria-label={t('subIssues.progress', { completed, total })} />
          </>
        )}
        <span className={s.grow} />
        {!readOnly && (
          <Button
            size="sm"
            variant="ghost"
            icon={<Plus size={14} aria-hidden />}
            aria-label={t('subIssues.addLabel')}
            onClick={() => openCreateIssue({ projectId: issue.project.id, parentId: issue.id, parentIdentifier: issue.identifier })}
          >
            {t('subIssues.add')}
          </Button>
        )}
      </div>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={nodes.map((n) => n.id)} strategy={verticalListSortingStrategy}>
          <ul className={s.childList}>
            {nodes.map((n) => (
              <ChildRow key={n.id} issue={n} readOnly={readOnly} />
            ))}
          </ul>
        </SortableContext>
      </DndContext>
    </section>
  )
}

function ChildRow({ issue, readOnly }: { issue: IssueRowFieldsFragment; readOnly: boolean }) {
  const { t } = useTranslation('issue')
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: issue.id, disabled: readOnly })
  const style = { transform: transform ? `translate3d(0, ${transform.y}px, 0)` : undefined, transition, opacity: isDragging ? 0.6 : 1 }
  return (
    <li ref={setNodeRef} style={style} className={s.childRow}>
      {!readOnly && (
        <button type="button" className={s.handle} aria-label={t('subIssues.reorder', { identifier: issue.identifier })} {...attributes} {...listeners}>
          <GripVertical size={14} aria-hidden />
        </button>
      )}
      <StateIcon type={issue.state.type} color={issue.state.color} label={issue.state.name} />
      <Link to={`/issue/${issue.identifier}`} className={s.childLink}>
        <span className={s.muted}>{issue.identifier}</span>
        <span className={s.childTitle}>{issue.title}</span>
      </Link>
      {issue.subIssueProgress.total > 0 && (
        <span className={s.muted}>
          {issue.subIssueProgress.completed}/{issue.subIssueProgress.total}
        </span>
      )}
      {issue.assignee && <Avatar id={issue.assignee.id} name={issue.assignee.name} size={18} />}
    </li>
  )
}
