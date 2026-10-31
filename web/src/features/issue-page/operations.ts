import { graphql } from '../../gql'

export const IssuePageQuery = graphql(`
  query IssuePage($id: String!) {
    issue(id: $id) {
      ...IssueRowFields
      number description createdAt updatedAt
      creator { id name }
      project { id key name archivedAt states { id name type color position isDefault } }
      children(first: 1) { totalCount }
    }
  }
`)

export const MoveIssueMutation = graphql(`
  mutation MoveIssue($id: ID!, $projectId: ID!) {
    moveIssue(id: $id, projectId: $projectId) { id identifier }
  }
`)

export const IssueHistoryQuery = graphql(`
  query IssueHistory($id: String!, $before: String) {
    issue(id: $id) {
      id
      history(last: 50, before: $before) {
        nodes { id action actor { id name } changes { field from to } createdAt }
        pageInfo { hasPreviousPage startCursor }
      }
    }
  }
`)

export const IssueRefQuery = graphql(`
  query IssueRef($id: String!) {
    issue(id: $id) { id identifier }
  }
`)
