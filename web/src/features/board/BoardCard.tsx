import { useSortable } from '@dnd-kit/sortable'
import { CornerDownRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import type { IssueRowFieldsFragment } from '../../gql/graphql'
import { Avatar } from '../../ui/Avatar'
import { PriorityIcon } from '../../ui/PriorityIcon'
import { isOverdue } from '../issues/filters'
import { LabelChips } from '../labels/LabelChips'
import s from './board.module.css'

type Props = { issue: IssueRowFieldsFragment; selected: boolean; onHover: () => void; draggable: boolean }

/** identifier, title (2 lines), priority, type and labels, assignee, due date, sub-issue count, parent badge. */
export function BoardCard({ issue, selected, onHover, draggable }: Props) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: issue.id, disabled: !draggable, data: { issue } })
  const style = {
    transform: transform ? `translate3d(${transform.x}px, ${transform.y}px, 0)` : undefined,
    transition,
    opacity: isDragging ? 0.5 : 1,
  }
  const href = `/issue/${issue.identifier}`
  const { role: _role, ...dragAttributes } = attributes
  return (
    <div
      ref={setNodeRef}
      style={style}
      className={[s.card, selected ? s.selected : ''].join(' ')}
      role="option"
      aria-selected={selected}
      data-row-id={issue.id}
      data-href={href}
      data-identifier={issue.identifier}
      data-project-id={issue.project.id}
      data-project-key={issue.project.key}
      onMouseEnter={onHover}
      {...dragAttributes}
      {...listeners}
    >
      <CardContent issue={issue} />
    </div>
  )
}

/** The card as shown under the pointer while dragging: no sortable registration and no data-row-id. */
export function BoardCardOverlay({ issue }: { issue: IssueRowFieldsFragment }) {
  return (
    <div className={[s.card, s.overlay].join(' ')} data-drag-overlay aria-hidden>
      <CardContent issue={issue} plainTitle />
    </div>
  )
}

/** `plainTitle` renders the title as text, for the overlay copy that must not hold a link. */
function CardContent({ issue, plainTitle = false }: { issue: IssueRowFieldsFragment; plainTitle?: boolean }) {
  const { t } = useTranslation('issues')
  const overdue = isOverdue(issue)
  const href = `/issue/${issue.identifier}`
  return (
    <>
      <div className={s.cardTop}>
        <span className={s.identifier}>{issue.identifier}</span>
        {issue.parent && (
          <span className={s.badge} title={t('list.parentBadge', { identifier: issue.parent.identifier })}>
            <CornerDownRight size={12} aria-hidden />
            {issue.parent.identifier}
          </span>
        )}
        <span className={s.grow} />
        {issue.assignee && <Avatar id={issue.assignee.id} name={issue.assignee.name} size={18} />}
      </div>
      {plainTitle ? (
        <span className={s.cardTitle}>{issue.title}</span>
      ) : (
        <Link to={href} className={s.cardTitle} draggable={false}>
          {issue.title}
        </Link>
      )}
      <div className={s.cardBottom}>
        <PriorityIcon priority={issue.priority} />
        <LabelChips type={issue.type ?? null} labels={issue.labels} />
        {issue.subIssueProgress.total > 0 && (
          <span className={s.meta} title={t('list.progress', { completed: issue.subIssueProgress.completed, total: issue.subIssueProgress.total })}>
            {issue.subIssueProgress.completed}/{issue.subIssueProgress.total}
          </span>
        )}
        {issue.dueDate && (
          <span className={[s.meta, overdue ? s.overdue : ''].join(' ')} title={t(overdue ? 'list.overdue' : 'list.dueDate', { date: issue.dueDate })}>
            {issue.dueDate}
          </span>
        )}
      </div>
    </>
  )
}
