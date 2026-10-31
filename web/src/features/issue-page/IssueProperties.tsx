import { X } from 'lucide-react'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { useMembers, useViewer } from '../../api/hooks'
import { formatDateTime } from '../../lib/date'
import { useIssueMutation } from '../../lib/issueCache'
import { Avatar } from '../../ui/Avatar'
import { Button } from '../../ui/Button'
import { Popover } from '../../ui/Popover'
import { priorityKey } from '../../ui/priority'
import { PriorityIcon } from '../../ui/PriorityIcon'
import { StateIcon } from '../../ui/StateIcon'
import { AssigneePicker } from '../pickers/AssigneePicker'
import { IssuePicker } from '../pickers/IssuePicker'
import { PriorityPicker } from '../pickers/PriorityPicker'
import { StatePicker } from '../pickers/StatePicker'
import { isDate } from '../issues/filters'
import s from './issue-page.module.css'
import type { PageIssue, PropName } from './types'

type Props = {
  issue: PageIssue
  readOnly: boolean
  open: PropName | null
  setOpen: (p: PropName | null) => void
  onMove: () => void
}

/** Right column: each property opens a picker; changes are optimistic. */
export function IssueProperties({ issue, readOnly, open, setOpen, onMove }: Props) {
  const { t } = useTranslation(['issue', 'common'])
  const update = useIssueMutation()
  const viewer = useViewer().data
  const users = useMembers().data ?? []
  const [anchors, setAnchors] = useState<Partial<Record<PropName, HTMLElement | null>>>({})
  const anchorRef = (p: PropName) => (el: HTMLElement | null) => {
    if (anchors[p] !== el) setAnchors((a) => ({ ...a, [p]: el }))
  }
  const close = () => setOpen(null)
  const value = (p: PropName, label: string, content: ReactNode) =>
    readOnly ? (
      <span className={s.propValue}>{content}</span>
    ) : (
      <button
        ref={anchorRef(p)}
        type="button"
        className={s.propButton}
        title={label}
        aria-haspopup="listbox"
        onClick={() => setOpen(open === p ? null : p)}
      >
        {content}
      </button>
    )

  const st = issue.state
  return (
    <aside className={s.props} aria-label={t('props.label')}>
      <dl>
        <dt>{t('props.state')}</dt>
        <dd>
          {value('state', t('props.state'), (
            <>
              <StateIcon type={st.type} color={st.color} />
              <span>{st.name}</span>
            </>
          ))}
        </dd>
        <dt>{t('props.priority')}</dt>
        <dd>
          {value('priority', t('props.priority'), (
            <>
              <PriorityIcon priority={issue.priority} decorative />
              <span>{t(`common:priority.${priorityKey(issue.priority)}`)}</span>
            </>
          ))}
        </dd>
        <dt>{t('props.assignee')}</dt>
        <dd>
          {value('assignee', t('props.assignee'), issue.assignee ? (
            <>
              <Avatar id={issue.assignee.id} name={issue.assignee.name} size={18} decorative />
              <span>{issue.assignee.name}</span>
            </>
          ) : (
            <span className={s.muted}>{t('common:pickers.noAssignee')}</span>
          ))}
        </dd>
        <dt>{t('props.dueDate')}</dt>
        <dd className={s.propRow}>
          <DueDateInput
            key={issue.dueDate ?? ''}
            value={issue.dueDate}
            label={t('props.dueDate')}
            disabled={readOnly}
            onCommit={(dueDate) => update.mutate({ id: issue.id, input: { dueDate }, optimistic: { dueDate } })}
          />
          {issue.dueDate && !readOnly && (
            <Button size="sm" variant="ghost" onClick={() => update.mutate({ id: issue.id, input: { dueDate: null }, optimistic: { dueDate: null } })}>
              {t('props.clearDue')}
            </Button>
          )}
        </dd>
        <dt>{t('props.parent')}</dt>
        <dd className={s.propRow}>
          {issue.parent ? (
            <>
              <Link to={`/issue/${issue.parent.identifier}`}>{issue.parent.identifier}</Link>
              {!readOnly && (
                <button
                  type="button"
                  className={s.iconButton}
                  aria-label={t('props.removeParent')}
                  onClick={() => update.mutate({ id: issue.id, input: { parentId: null }, optimistic: { parent: null } })}
                >
                  <X size={12} aria-hidden />
                </button>
              )}
            </>
          ) : readOnly ? (
            <span className={s.muted}>{t('props.noParent')}</span>
          ) : (
            <button ref={anchorRef('parent')} type="button" className={s.propButton} onClick={() => setOpen(open === 'parent' ? null : 'parent')}>
              <span className={s.muted}>{t('props.setParent')}</span>
            </button>
          )}
        </dd>
        <dt>{t('props.project')}</dt>
        <dd className={s.propRow}>
          <Link to={`/p/${issue.project.key}`}>{issue.project.key}</Link>
          {!readOnly && (
            <Button size="sm" variant="ghost" onClick={onMove}>
              {t('props.move')}
            </Button>
          )}
        </dd>
        <dt>{t('props.created')}</dt>
        <dd>{t('props.createdBy', { name: issue.creator.name, date: formatDateTime(issue.createdAt) })}</dd>
        <dt>{t('props.updated')}</dt>
        <dd>{formatDateTime(issue.updatedAt)}</dd>
      </dl>

      <Popover anchor={open ? (anchors[open] ?? null) : null} open={!!open && !readOnly && !!anchors[open]} onClose={close} placement="bottom-start">
        {open === 'state' && (
          <StatePicker
            states={issue.project.states}
            value={st.id}
            onClose={close}
            onSelect={(stateId) => {
              const next = issue.project.states.find((x) => x.id === stateId)
              close()
              if (stateId === st.id || !next) return
              update.mutate({ id: issue.id, input: { stateId }, optimistic: { state: { id: next.id, name: next.name, type: next.type, color: next.color } } })
            }}
          />
        )}
        {open === 'priority' && (
          <PriorityPicker
            value={issue.priority}
            onClose={close}
            onSelect={(priority) => {
              close()
              if (priority !== issue.priority) update.mutate({ id: issue.id, input: { priority }, optimistic: { priority } })
            }}
          />
        )}
        {open === 'assignee' && viewer && (
          <AssigneePicker
            users={users}
            viewerId={viewer.id}
            value={issue.assignee?.id ?? null}
            onClose={close}
            onSelect={(userId) => {
              close()
              const u = users.find((x) => x.id === userId)
              update.mutate({ id: issue.id, input: { assigneeId: userId }, optimistic: { assignee: u ? { id: u.id, name: u.name } : null } })
            }}
          />
        )}
        {open === 'parent' && (
          <IssuePicker
            projectId={issue.project.id}
            excludeId={issue.id}
            value={issue.parent?.id ?? null}
            onClose={close}
            onSelect={(p) => {
              close()
              update.mutate({ id: issue.id, input: { parentId: p.id }, optimistic: { parent: { id: p.id, identifier: p.identifier } } })
            }}
          />
        )}
      </Popover>
    </aside>
  )
}

/**
 * Native date field that saves when left (blur or Enter), not on every input
 * event: typing a year fires one event per digit (0002, 0020, 0202, 2026).
 */
function DueDateInput({ value, label, disabled, onCommit }: { value: string | null; label: string; disabled: boolean; onCommit: (d: string | null) => void }) {
  const [draft, setDraft] = useState(value ?? '')
  const commit = () => {
    const next = draft || null
    if (next === value || (next !== null && !isDate(next))) return
    onCommit(next)
  }
  return (
    <input
      type="date"
      aria-label={label}
      disabled={disabled}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur()
      }}
    />
  )
}
