import { useQueryClient } from '@tanstack/react-query'

/** Refetches every project query (sidebar, browse page, current project). */
export function useInvalidateProjects() {
  const qc = useQueryClient()
  return () =>
    Promise.all(['project', 'projects', 'browseProjects', 'issues'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
}
