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
