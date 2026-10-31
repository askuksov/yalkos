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
}

/** An issue as the list fragment returns it. */
export function issueRow(n: number, over: Partial<Row> = {}): Row {
  const st = states[1]
  return {
    id: `i${n}`, identifier: `YLK-${n}`, title: `Issue ${n}`, priority: 0, dueDate: null, sortOrder: n * 1000,
    state: { id: st.id, name: st.name, type: st.type, color: st.color },
    assignee: null, parent: null, subIssueProgress: { completed: 0, total: 0 },
    project: { id: 'p1', key: 'YLK', archivedAt: null },
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
