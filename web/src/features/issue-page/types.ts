import type { IssuePageQuery } from '../../gql/graphql'

export type PageIssue = IssuePageQuery['issue']
export type PropName = 'state' | 'assignee' | 'priority' | 'parent' | 'type' | 'labels' | 'milestone'
