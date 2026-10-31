import { graphql } from '../../gql'

/** Fields of an issue row (list, board card, sub-issue). */
export const IssueRowFields = graphql(`
  fragment IssueRowFields on Issue {
    id identifier title priority dueDate sortOrder
    state { id name type color }
    assignee { id name }
    parent { id identifier }
    subIssueProgress { completed total }
    project { id key archivedAt }
    type { id name color }
    labels { id name color }
    milestone { id name }
  }
`)

export const IssueGroupQuery = graphql(`
  query IssueGroup($filter: IssueFilter, $orderBy: IssueOrder, $first: Int, $after: String) {
    issues(filter: $filter, orderBy: $orderBy, first: $first, after: $after) {
      totalCount
      nodes { ...IssueRowFields }
      pageInfo { hasNextPage endCursor }
    }
  }
`)

export const IssueChildrenQuery = graphql(`
  query IssueChildren($id: String!, $orderBy: IssueOrder) {
    issue(id: $id) {
      id
      # 200 rows keep the query under the API cost limit (each row carries labels).
      children(first: 200, orderBy: $orderBy) { nodes { ...IssueRowFields } }
    }
  }
`)

export const UpdateIssueMutation = graphql(`
  mutation UpdateIssue($id: ID!, $input: UpdateIssueInput!) {
    updateIssue(id: $id, input: $input) { ...IssueRowFields }
  }
`)

export const DeleteIssueMutation = graphql(`
  mutation DeleteIssue($id: ID!) {
    deleteIssue(id: $id)
  }
`)

export const UnarchiveProjectMutation = graphql(`
  mutation UnarchiveProject($id: ID!) {
    unarchiveProject(id: $id) { id archivedAt }
  }
`)
