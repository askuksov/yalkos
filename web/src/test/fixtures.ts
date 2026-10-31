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
