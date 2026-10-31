import { useOutletContext } from 'react-router'

import type { ProjectQuery } from '../gql/graphql'

export type CurrentProject = ProjectQuery['project']

/** The project of the current /p/:key route (set by ProjectRoute). */
export function useCurrentProject(): CurrentProject {
  return useOutletContext<{ project: CurrentProject }>().project
}
