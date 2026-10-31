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

export const IssueTimelineQuery = graphql(`
  query IssueTimeline($id: String!, $before: String) {
    issue(id: $id) {
      id
      timeline(last: 50, before: $before) {
        nodes {
          __typename
          ... on Comment { id body createdAt editedAt author { id name } }
          ... on HistoryEntry { id action actor { id name } changes { field from to } createdAt }
        }
        pageInfo { hasPreviousPage startCursor }
      }
    }
  }
`)

export const IssueAttachmentsQuery = graphql(`
  query IssueAttachments($id: String!) {
    issue(id: $id) {
      id
      attachments { id name size contentType url inline createdAt uploader { id name } }
    }
  }
`)

export const CreateCommentMutation = graphql(`
  mutation CreateComment($issueId: ID!, $body: String!, $attachmentIds: [ID!]) {
    createComment(issueId: $issueId, body: $body, attachmentIds: $attachmentIds) { id body }
  }
`)

export const UpdateCommentMutation = graphql(`
  mutation UpdateComment($id: ID!, $body: String!) {
    updateComment(id: $id, body: $body) { id body editedAt }
  }
`)

export const DeleteCommentMutation = graphql(`
  mutation DeleteComment($id: ID!) {
    deleteComment(id: $id)
  }
`)

export const DeleteAttachmentMutation = graphql(`
  mutation DeleteAttachment($id: ID!) {
    deleteAttachment(id: $id)
  }
`)

export const IssueRefQuery = graphql(`
  query IssueRef($id: String!) {
    issue(id: $id) { id identifier }
  }
`)
