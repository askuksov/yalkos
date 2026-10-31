import { useQuery } from '@tanstack/react-query'

import { request } from '../../api/client'
import { MilestonesQuery } from './operations'

/** The milestones of a project (UUID or key), in manual order. */
export function useMilestones(project: string | null | undefined) {
  return useQuery({
    queryKey: ['milestones', project],
    queryFn: () => request(MilestonesQuery, { projectId: project ?? '' }),
    enabled: !!project,
    select: (d) => d.project.milestones,
  })
}
