import type { TFunction } from 'i18next'

import { priorityKey } from '../../ui/priority'

export type HistoryEntry = {
  id: string
  action: string
  actor: { id: string; name: string } | null
  changes: { field: string; from: string | null; to: string | null }[]
  createdAt: string
}

export type DescribeContext = {
  t: TFunction<['issue', 'common']>
  viewerId?: string
  users: Map<string, string>
  states: Map<string, string>
  issues: Map<string, string>
  projects: Map<string, string>
  /** Issue number right before each entry (by entry id), for moves that keep the number. */
  numberAt?: Map<string, number>
}

/** Change values are JSON text; null when absent. */
function value(raw: string | null): unknown {
  if (raw === null) return null
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

/** Fields that never get a line of their own. */
const hidden = new Set(['sortOrder', 'number', 'completedAt', 'canceledAt'])

/**
 * Human lines for one history entry (spec wording), one per shown field. A
 * project change is one move line that names the former identifier.
 */
export function describe(entry: HistoryEntry, ctx: DescribeContext): string[] {
  const { t } = ctx
  const actor = entry.actor?.name ?? t('activity.someone')
  if (entry.action === 'create') return [t('activity.created', { actor })]
  if (entry.action === 'delete') return [t('activity.deleted', { actor })]

  const byField = new Map(entry.changes.map((c) => [c.field, c]))
  const user = (id: unknown) => (typeof id === 'string' && ctx.users.get(id)) || t('activity.someone')
  const state = (id: unknown) => (typeof id === 'string' && ctx.states.get(id)) || t('activity.deletedState')
  const project = (id: unknown) => (typeof id === 'string' && ctx.projects.get(id)) || t('activity.unknownProject')
  const lines: string[] = []
  const moved = byField.get('projectId')

  if (moved) {
    const from = project(value(moved.from))
    const number = value(byField.get('number')?.from ?? null) ?? ctx.numberAt?.get(entry.id)
    lines.push(t('activity.moved', { actor, from, to: project(value(moved.to)), was: `${from}-${String(number ?? '?')}` }))
  }
  for (const c of entry.changes) {
    if (hidden.has(c.field) || c.field === 'projectId') continue
    const from = value(c.from)
    const to = value(c.to)
    switch (c.field) {
      case 'title':
        lines.push(t('activity.renamed', { actor, title: String(to ?? '') }))
        break
      case 'description':
        lines.push(t('activity.description', { actor }))
        break
      case 'stateId':
        if (!moved) lines.push(t('activity.state', { actor, from: state(from), to: state(to) }))
        break
      case 'priority':
        lines.push(to ? t('activity.priority', { actor, priority: t(`common:priority.${priorityKey(Number(to))}`) }) : t('activity.priorityRemoved', { actor }))
        break
      case 'assigneeId':
        if (to === null) lines.push(t('activity.unassigned', { actor, user: user(from) }))
        else if (entry.actor && to === entry.actor.id) lines.push(t('activity.assignedSelf', { actor }))
        else lines.push(t('activity.assigned', { actor, user: user(to) }))
        break
      case 'dueDate':
        lines.push(to ? t('activity.due', { actor, date: String(to) }) : t('activity.dueRemoved', { actor }))
        break
      case 'parentId':
        lines.push(
          to ? t('activity.parent', { actor, parent: (typeof to === 'string' && ctx.issues.get(to)) || t('activity.unknownIssue') }) : t('activity.parentRemoved', { actor }),
        )
        break
    }
  }
  return lines
}

/**
 * The issue's number before each entry, walking back from the current number:
 * an entry that changed the number had its "from" before it.
 */
export function numbersBefore(entries: HistoryEntry[], current: number): Map<string, number> {
  const out = new Map<string, number>()
  let n = current
  for (let i = entries.length - 1; i >= 0; i--) {
    const c = entries[i].changes.find((x) => x.field === 'number')
    const from = c ? value(c.from) : null
    if (typeof from === 'number') n = from
    out.set(entries[i].id, n)
  }
  return out
}

/** Ids an entry list refers to, for loading names. */
export function referencedIds(entries: HistoryEntry[], field: string): string[] {
  const out = new Set<string>()
  for (const e of entries) {
    if (e.action !== 'update') continue
    for (const c of e.changes) {
      if (c.field !== field) continue
      for (const v of [value(c.from), value(c.to)]) if (typeof v === 'string') out.add(v)
    }
  }
  return [...out]
}
