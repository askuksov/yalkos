import type { IssueFilter, StateType } from '../../gql/graphql'

/** Schema enums IssueOrderField and OrderDirection. */
export type IssueOrderField = 'SORT_ORDER' | 'CREATED_AT' | 'UPDATED_AT' | 'PRIORITY' | 'DUE_DATE'
export type OrderDirection = 'ASC' | 'DESC'

// Filters and display options of an issue view, kept in the URL query string:
// ?state=<ids>&type=started,unstarted&assignee=me,none,<ids>&priority=1,2
// &kind=<type label ids>|none&label=<label ids>&nolabel=1&milestone=<id>|none
// &due=overdue|week|none|<YYYY-MM-DD>&parent=none&children=1&q=<text>
// &order=priority&dir=desc&nest=0. Invalid values are dropped silently.
// "type" is the state type; the issue type (a label) is "kind".

export type ViewParams = {
  state: string[]
  type: StateType[]
  /** 'me', 'none' or user ids; 'none' never mixes with people. */
  assignee: string[]
  priority: number[]
  /** Issue type label ids, or ['none'] for issues without a type. */
  kind: string[]
  /** Label ids: issues with any of them. */
  label: string[]
  /** Issues without labels. */
  nolabel?: boolean
  /** A milestone id, or 'none' for issues outside milestones. */
  milestone?: string
  /** overdue, week, none, or a date: due before it. */
  due?: string
  parent?: 'none'
  children?: boolean
  q?: string
  order: IssueOrderField
  dir: OrderDirection
  nest: boolean
  view: 'list' | 'board'
}

const stateTypes: StateType[] = ['BACKLOG', 'UNSTARTED', 'STARTED', 'COMPLETED', 'CANCELED']

const orderNames: Record<string, IssueOrderField> = {
  manual: 'SORT_ORDER',
  priority: 'PRIORITY',
  updated: 'UPDATED_AT',
  created: 'CREATED_AT',
  due: 'DUE_DATE',
}
const orderParam = Object.fromEntries(Object.entries(orderNames).map(([k, v]) => [v, k])) as Record<IssueOrderField, string>

export const orderFields = Object.values(orderNames)

function list(sp: URLSearchParams, key: string): string[] {
  const v = sp.get(key)
  if (!v) return []
  return [...new Set(v.split(',').map((s) => s.trim()).filter(Boolean))]
}

export function isDate(s: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return false
  const [y, m, d] = s.split('-').map(Number)
  const dt = new Date(Date.UTC(y, m - 1, d))
  return dt.getUTCFullYear() === y && dt.getUTCMonth() === m - 1 && dt.getUTCDate() === d
}

export function parseParams(sp: URLSearchParams, view: 'list' | 'board' = 'list'): ViewParams {
  let assignee = list(sp, 'assignee')
  if (assignee.includes('none')) assignee = ['none']
  let kind = list(sp, 'kind')
  if (kind.includes('none')) kind = ['none']
  const due = sp.get('due') ?? undefined
  const q = sp.get('q')?.trim()
  return {
    state: list(sp, 'state'),
    type: list(sp, 'type')
      .map((t) => t.toUpperCase() as StateType)
      .filter((t) => stateTypes.includes(t)),
    assignee,
    priority: list(sp, 'priority')
      .filter((p) => /^[0-4]$/.test(p))
      .map(Number),
    kind,
    label: list(sp, 'label'),
    nolabel: sp.get('nolabel') === '1' ? true : undefined,
    milestone: sp.get('milestone') || undefined,
    due: due && (['overdue', 'week', 'none'].includes(due) || isDate(due)) ? due : undefined,
    parent: sp.get('parent') === 'none' ? 'none' : undefined,
    children: sp.get('children') === '1' ? true : undefined,
    q: q || undefined,
    order: orderNames[sp.get('order') ?? ''] ?? 'SORT_ORDER',
    dir: sp.get('dir') === 'desc' ? 'DESC' : 'ASC',
    nest: sp.get('nest') !== '0',
    view,
  }
}

const enc = (vals: (string | number)[]) => vals.map((v) => encodeURIComponent(String(v))).join(',')

/** Query string without "?"; defaults are omitted, keys in a stable order. */
export function toSearch(p: ViewParams): string {
  const parts: string[] = []
  if (p.state.length) parts.push(`state=${enc(p.state)}`)
  if (p.type.length) parts.push(`type=${enc(p.type.map((t) => t.toLowerCase()))}`)
  if (p.assignee.length) parts.push(`assignee=${enc(p.assignee)}`)
  if (p.priority.length) parts.push(`priority=${enc(p.priority)}`)
  if (p.kind.length) parts.push(`kind=${enc(p.kind)}`)
  if (p.label.length) parts.push(`label=${enc(p.label)}`)
  if (p.nolabel) parts.push('nolabel=1')
  if (p.milestone) parts.push(`milestone=${encodeURIComponent(p.milestone)}`)
  if (p.due) parts.push(`due=${encodeURIComponent(p.due)}`)
  if (p.parent) parts.push('parent=none')
  if (p.children) parts.push('children=1')
  if (p.q) parts.push(`q=${encodeURIComponent(p.q)}`)
  if (p.order !== 'SORT_ORDER') parts.push(`order=${orderParam[p.order]}`)
  if (p.dir !== 'ASC') parts.push('dir=desc')
  if (!p.nest) parts.push('nest=0')
  return parts.join('&')
}

/** Whether any filter (not a display option) is set. */
export function hasFilters(p: ViewParams): boolean {
  return !!(p.state.length || p.type.length || p.assignee.length || p.priority.length || p.kind.length || p.label.length || p.nolabel || p.milestone ||
    p.due || p.parent || p.children || p.q)
}

/** Adds days to a YYYY-MM-DD date. */
export function addDays(date: string, days: number): string {
  const [y, m, d] = date.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d + days)).toISOString().slice(0, 10)
}

/** Today in local time, YYYY-MM-DD. */
export function today(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/**
 * The API filter for the params. Conditions are AND-ed by the API, so "me"
 * together with other people becomes one "in" list (needs viewerId).
 */
export function toFilter(p: ViewParams, todayDate: string, viewerId?: string): IssueFilter {
  const f: IssueFilter = {}
  if (p.state.length) f.state = { in: p.state }
  if (p.type.length) f.stateType = { in: p.type }
  if (p.assignee.length) {
    const people = p.assignee.filter((a) => a !== 'me' && a !== 'none')
    if (p.assignee.includes('none')) f.assignee = { null: true }
    else if (!people.length) f.assignee = { isMe: true }
    else if (p.assignee.includes('me') && viewerId) f.assignee = { in: [viewerId, ...people.filter((x) => x !== viewerId)] }
    else if (p.assignee.includes('me')) f.assignee = { isMe: true }
    else f.assignee = { in: people }
  }
  if (p.priority.length) f.priority = { in: p.priority }
  if (p.kind.length) f.type = p.kind.includes('none') ? { null: true } : { in: p.kind }
  if (p.nolabel) f.label = { null: true }
  else if (p.label.length) f.label = { in: p.label }
  if (p.milestone) f.milestone = p.milestone === 'none' ? { null: true } : { eq: p.milestone }
  switch (p.due) {
    case undefined:
      break
    case 'overdue':
      f.dueDate = { lt: todayDate }
      f.stateType = { ...f.stateType, nin: ['COMPLETED', 'CANCELED'] }
      break
    case 'week':
      f.dueDate = { gte: todayDate, lte: addDays(todayDate, 6) }
      break
    case 'none':
      f.dueDate = { null: true }
      break
    default:
      f.dueDate = { lt: p.due }
  }
  if (p.parent) f.parent = { null: true }
  if (p.children) f.hasChildren = true
  if (p.q) f.search = p.q
  return f
}

/** Past its due date and not completed or canceled. */
export function isOverdue(i: { dueDate: string | null; state: { type: StateType } }): boolean {
  return !!i.dueDate && i.dueDate < today() && i.state.type !== 'COMPLETED' && i.state.type !== 'CANCELED'
}
