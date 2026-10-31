import { useQuery } from '@tanstack/react-query'

import { request } from './client'
import { MembersQuery, ProjectQuery, ProjectsQuery, ViewerQuery } from './operations'

export function useViewer() {
  return useQuery({ queryKey: ['viewer'], queryFn: () => request(ViewerQuery), select: (d) => d.viewer })
}

/** Whether the viewer manages projects, states and members. */
export function useIsAdmin(): boolean {
  const role = useViewer().data?.role
  return role === 'OWNER' || role === 'ADMIN'
}

export function useProjects({ includeArchived = false }: { includeArchived?: boolean } = {}) {
  return useQuery({
    queryKey: ['projects', { includeArchived }],
    queryFn: () => request(ProjectsQuery, { includeArchived }),
    select: (d) => d.projects.nodes,
  })
}

/** A project by UUID, key or former key. */
export function useProject(key: string | undefined) {
  return useQuery({
    queryKey: ['project', key],
    queryFn: () => request(ProjectQuery, { id: key ?? '' }),
    enabled: !!key,
    select: (d) => d.project,
  })
}

/** Workspace users. */
export function useMembers() {
  return useQuery({ queryKey: ['members'], queryFn: () => request(MembersQuery), select: (d) => d.members })
}
