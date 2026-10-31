import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, CornerDownRight, MessageSquare, Target } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { request } from '../../api/client'
import type { IssueOrder, IssueRowFieldsFragment } from '../../gql/graphql'
import { Avatar } from '../../ui/Avatar'
import { PriorityIcon } from '../../ui/PriorityIcon'
import { StateIcon } from '../../ui/StateIcon'
import { LabelChips } from '../labels/LabelChips'
import ms from '../milestones/milestones.module.css'
import { isOverdue } from './filters'
import { IssueChildrenQuery } from './fragments'
import s from './issues.module.css'
import { useSelectionContext } from './selection'

type Props = {
  issue: IssueRowFieldsFragment
  depth?: number
  /** Nested mode: rows with sub-issues expand; flat mode shows a parent badge. */
  nested: boolean
  orderBy: IssueOrder
}

/** [priority] [identifier] [state] title [2/5] [parent badge] [type + labels] … [due] [assignee] */
export function IssueRow({ issue, depth = 0, nested, orderBy }: Props) {
  const { t } = useTranslation('issues')
  const sel = useSelectionContext()
  const [expanded, setExpanded] = useState(false)
  const progress = issue.subIssueProgress
  const canExpand = nested && progress.total > 0
  const overdue = isOverdue(issue)
  const href = `/issue/${issue.identifier}`
  return (
    <>
      <div
        className={[s.row, sel.selected === issue.id ? s.rowSelected : ''].join(' ')}
        role="option"
        aria-selected={sel.selected === issue.id}
        data-row-id={issue.id}
        data-href={href}
        data-identifier={issue.identifier}
        data-project-id={issue.project.id}
        data-project-key={issue.project.key}
        style={{ paddingInlineStart: 12 + depth * 22 }}
        onMouseEnter={() => sel.setSelected(issue.id)}
      >
        {sel.checkable && (
          <input
            type="checkbox"
            className={[s.check, sel.checked.size ? s.checkVisible : ''].join(' ')}
            aria-label={t('list.select', { identifier: issue.identifier })}
            checked={sel.checked.has(issue.id)}
            onChange={() => sel.toggle(issue.id)}
          />
        )}
        <span className={s.chevronSlot}>
          {canExpand && (
            <button
              type="button"
              className={s.chevron}
              aria-expanded={expanded}
              aria-label={t(expanded ? 'list.hideSubIssues' : 'list.showSubIssues', { identifier: issue.identifier })}
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? <ChevronDown size={14} aria-hidden /> : <ChevronRight size={14} aria-hidden />}
            </button>
          )}
        </span>
        <PriorityIcon priority={issue.priority} />
        <Link to={href} className={s.rowLink}>
          <span className={s.identifier}>{issue.identifier}</span>
          <StateIcon type={issue.state.type} color={issue.state.color} label={issue.state.name} />
          <span className={s.title}>{issue.title}</span>
        </Link>
        {progress.total > 0 && (
          <span className={s.progress} title={t('list.progress', { completed: progress.completed, total: progress.total })}>
            {progress.completed}/{progress.total}
          </span>
        )}
        {!nested && issue.parent && (
          <span className={s.badge} title={t('list.parentBadge', { identifier: issue.parent.identifier })}>
            <CornerDownRight size={12} aria-hidden />
            {issue.parent.identifier}
          </span>
        )}
        <LabelChips type={issue.type ?? null} labels={issue.labels} />
        {issue.milestone && (
          <span className={ms.chip} title={t('list.milestone', { name: issue.milestone.name })}>
            <Target size={12} aria-hidden />
            {issue.milestone.name}
          </span>
        )}
        <span className={s.grow} />
        {issue.commentCount > 0 && (
          <span className={s.progress} title={t('list.comments', { count: issue.commentCount })}>
            <MessageSquare size={12} aria-hidden /> {issue.commentCount}
          </span>
        )}
        {issue.dueDate && (
          <span className={[s.due, overdue ? s.overdue : ''].join(' ')} title={t(overdue ? 'list.overdue' : 'list.dueDate', { date: issue.dueDate })}>
            {issue.dueDate}
          </span>
        )}
        <span className={s.assignee}>{issue.assignee && <Avatar id={issue.assignee.id} name={issue.assignee.name} />}</span>
      </div>
      {canExpand && expanded && <SubIssueRows parentId={issue.id} depth={depth + 1} orderBy={orderBy} />}
    </>
  )
}

/** Direct children of an expanded row, any state, indented. */
function SubIssueRows({ parentId, depth, orderBy }: { parentId: string; depth: number; orderBy: IssueOrder }) {
  const children = useQuery({
    queryKey: ['children', parentId, orderBy.field, orderBy.direction],
    queryFn: () => request(IssueChildrenQuery, { id: parentId, orderBy }),
  })
  return (
    <>
      {children.data?.issue.children.nodes.map((c) => (
        <IssueRow key={c.id} issue={c} depth={depth} nested orderBy={orderBy} />
      ))}
    </>
  )
}
