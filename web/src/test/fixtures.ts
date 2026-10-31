import { GraphQLRequestError } from '../api/client'

export const owner = { id: 'u1', name: 'Olga', email: 'o@example.com', role: 'OWNER' }
export const member = { id: 'u2', name: 'Andrii', email: 'a@example.com', role: 'MEMBER' }

export const states = [
  { id: 's-backlog', name: 'Backlog', type: 'BACKLOG', color: '#999999', position: 0, isDefault: false },
  { id: 's-todo', name: 'Todo', type: 'UNSTARTED', color: '#888888', position: 1, isDefault: true },
  { id: 's-progress', name: 'In Progress', type: 'STARTED', color: '#f2c94c', position: 2, isDefault: false },
  { id: 's-done', name: 'Done', type: 'COMPLETED', color: '#5e6ad2', position: 3, isDefault: false },
  { id: 's-canceled', name: 'Canceled', type: 'CANCELED', color: '#95a2b3', position: 4, isDefault: false },
]

export function project(over: Record<string, unknown> = {}) {
  return {
    id: 'p1', key: 'YLK', name: 'Yalkos', description: '', icon: null, color: null, archivedAt: null, isMember: true,
    states, members: [{ id: owner.id, name: owner.name }, { id: member.id, name: member.name }],
    ...over,
  }
}

export const authLost = () => {
  throw new GraphQLRequestError('AUTHENTICATION_ERROR', 'Authentication required')
}

/** Handlers for a signed-in viewer with the given projects. */
export function signedInAs(viewer: typeof owner, projects: ReturnType<typeof project>[] = [project()]) {
  return {
    SetupStatus: () => ({ setupStatus: { needed: false } }),
    Viewer: () => ({ viewer }),
    Members: () => ({ members: [owner, member].map((u) => ({ ...u, disabled: false })) }),
    Projects: (vars: Record<string, unknown> | undefined) => ({
      projects: { nodes: projects.filter((p) => vars?.includeArchived || !p.archivedAt) },
    }),
    Project: (vars: Record<string, unknown> | undefined) => {
      const p = projects.find((x) => x.key === vars?.id || x.id === vars?.id)
      if (!p) throw new GraphQLRequestError('NOT_FOUND', 'Project not found')
      return { project: p }
    },
  }
}

type Row = {
  id: string
  identifier: string
  title: string
  priority: number
  dueDate: string | null
  sortOrder: number
  state: { id: string; name: string; type: string; color: string }
  assignee: { id: string; name: string } | null
  parent: { id: string; identifier: string } | null
  subIssueProgress: { completed: number; total: number }
  project: { id: string; key: string; archivedAt: string | null }
  type: { id: string; name: string; color: string } | null
  labels: { id: string; name: string; color: string }[]
  milestone: { id: string; name: string } | null
  commentCount: number
}

/** An issue as the list fragment returns it. */
export function issueRow(n: number, over: Partial<Row> = {}): Row {
  const st = states[1]
  return {
    id: `i${n}`, identifier: `YLK-${n}`, title: `Issue ${n}`, priority: 0, dueDate: null, sortOrder: n * 1000,
    state: { id: st.id, name: st.name, type: st.type, color: st.color },
    assignee: null, parent: null, subIssueProgress: { completed: 0, total: 0 },
    project: { id: 'p1', key: 'YLK', archivedAt: null },
    type: null, labels: [], milestone: null, commentCount: 0,
    ...over,
  }
}

export function stateOf(id: string) {
  const st = states.find((s) => s.id === id) ?? states[1]
  return { id: st.id, name: st.name, type: st.type, color: st.color }
}

/** An issues connection page. */
export function conn<T>(nodes: T[], { total = nodes.length, next = '' }: { total?: number; next?: string } = {}) {
  return { issues: { totalCount: total, nodes, pageInfo: { hasNextPage: !!next, endCursor: next || null } } }
}

/** An issue as the issue page query returns it. */
export function pageIssue(n: number, over: Record<string, unknown> = {}) {
  return {
    ...issueRow(n),
    number: n,
    description: '',
    createdAt: '2026-10-30T12:02:00Z',
    updatedAt: '2026-10-30T14:40:00Z',
    creator: { id: owner.id, name: owner.name },
    project: { id: 'p1', key: 'YLK', name: 'Yalkos', archivedAt: null as string | null, states },
    children: { totalCount: 0 },
    ...over,
  }
}

type LabelFixture = {
  id: string; name: string; color: string; description: string; isGroup: boolean; exclusive: boolean
  position: number; parent: { id: string } | null; children: LabelFixture[]
}

function lbl(id: string, name: string, color: string, position: number, parent: string | null): LabelFixture {
  return { id, name, color, description: '', isGroup: false, exclusive: false, position, parent: parent ? { id: parent } : null, children: [] }
}

/** Type group (Bug, Feature, Task, Improvement), group Area (Backend, Frontend), free Performance and Security. */
export function labelsData({ defaultType = 'task' as string | null } = {}) {
  const types = [lbl('bug', 'Bug', '#e5484d', 0, 'type'), lbl('feature', 'Feature', '#8e4ec6', 1, 'type'), lbl('task', 'Task', '#6e7781', 2, 'type'), lbl('improvement', 'Improvement', '#30a46c', 3, 'type')]
  const area = lbl('area', 'Area', '#3a7bd5', 1000, null)
  area.isGroup = true
  area.exclusive = true
  area.children = [lbl('backend', 'Backend', '#3a7bd5', 1000, 'area'), lbl('frontend', 'Frontend', '#26b5ce', 2000, 'area')]
  const typeGroup = { ...lbl('type', 'Type', '#6e7781', 0, null), isGroup: true, exclusive: true, children: types }
  return {
    labels: [typeGroup, area, lbl('perf', 'Performance', '#f2994a', 2000, null), lbl('security', 'Security', '#eb5757', 3000, null)],
    labelSettings: { typeGroup: { id: 'type' }, defaultType: defaultType ? { id: defaultType } : null },
  }
}

/** A label as issue rows carry it. */
export function chip(id: string) {
  const all = labelsData().labels.flatMap((l) => [l, ...l.children])
  const l = all.find((x) => x.id === id)
  if (!l) throw new Error(`no label ${id}`)
  return { id: l.id, name: l.name, color: l.color }
}

/** A milestone as the milestone fragment returns it. */
export function milestone(n: number, over: Record<string, unknown> = {}) {
  return {
    id: `m${n}`, name: `Milestone ${n}`, targetDate: null as string | null, status: 'PLANNED', statusAutomatic: true, overdue: false,
    progress: { completed: 0, total: 0 }, lead: null as { id: string; name: string } | null, project: { id: 'p1', key: 'YLK' },
    ...over,
  }
}

/** A timeline page as IssueTimeline returns it. */
export function timeline(nodes: unknown[], { older = '' }: { older?: string } = {}) {
  return { issue: { id: 'i1', timeline: { nodes, pageInfo: { hasPreviousPage: !!older, startCursor: older || null } } } }
}

export function event(id: string, changes: [string, unknown, unknown][], { action = 'update', at = '2026-10-30T12:00:00Z', actor = owner as { id: string; name: string } | null } = {}) {
  const j = (v: unknown) => (v === null ? null : JSON.stringify(v))
  return { __typename: 'HistoryEntry', id, action, actor: actor && { id: actor.id, name: actor.name }, createdAt: at, changes: changes.map(([field, from, to]) => ({ field, from: j(from), to: j(to) })) }
}

export function comment(id: string, body: string, { at = '2026-10-30T12:05:00Z', author = owner as { id: string; name: string }, editedAt = null as string | null } = {}) {
  return { __typename: 'Comment', id, body, createdAt: at, editedAt, author: { id: author.id, name: author.name } }
}
