import type { MilestoneStatus } from '../../gql/graphql'

export const statuses: MilestoneStatus[] = ['PLANNED', 'IN_PROGRESS', 'DONE', 'CANCELED']

/** Catalog key of a status (issues:milestones.status.<key>). */
export const statusKey = (st: MilestoneStatus) => st.toLowerCase() as Lowercase<MilestoneStatus>
