import { graphql } from '../../gql'

export const BrowseProjectsQuery = graphql(`
  query BrowseProjects($includeArchived: Boolean) {
    projects(includeArchived: $includeArchived, first: 100) {
      nodes { id key name description color archivedAt isMember members { id } }
    }
  }
`)

export const CreateProjectMutation = graphql(`
  mutation CreateProject($input: CreateProjectInput!) {
    createProject(input: $input) { id key }
  }
`)

export const UpdateProjectMutation = graphql(`
  mutation UpdateProject($id: ID!, $input: UpdateProjectInput!) {
    updateProject(id: $id, input: $input) { id key name description icon color }
  }
`)

export const ArchiveProjectMutation = graphql(`
  mutation ArchiveProject($id: ID!) {
    archiveProject(id: $id) { id archivedAt }
  }
`)

export const DeleteProjectMutation = graphql(`
  mutation DeleteProject($id: ID!, $confirmKey: String!) {
    deleteProject(id: $id, confirmKey: $confirmKey)
  }
`)

export const AddProjectMemberMutation = graphql(`
  mutation AddProjectMember($projectId: ID!, $userId: ID!) {
    addProjectMember(projectId: $projectId, userId: $userId) { id isMember }
  }
`)

export const RemoveProjectMemberMutation = graphql(`
  mutation RemoveProjectMember($projectId: ID!, $userId: ID!) {
    removeProjectMember(projectId: $projectId, userId: $userId) { id isMember }
  }
`)

export const CreateStateMutation = graphql(`
  mutation CreateState($projectId: ID!, $input: CreateStateInput!) {
    createState(projectId: $projectId, input: $input) { id }
  }
`)

export const UpdateStateMutation = graphql(`
  mutation UpdateState($id: ID!, $input: UpdateStateInput!) {
    updateState(id: $id, input: $input) { id name color isDefault }
  }
`)

export const ReorderStatesMutation = graphql(`
  mutation ReorderStates($projectId: ID!, $stateIds: [ID!]!) {
    reorderStates(projectId: $projectId, stateIds: $stateIds) { id position }
  }
`)

export const DeleteStateMutation = graphql(`
  mutation DeleteState($id: ID!, $moveIssuesTo: ID) {
    deleteState(id: $id, moveIssuesTo: $moveIssuesTo)
  }
`)

export const StateIssueCountQuery = graphql(`
  query StateIssueCount($filter: IssueFilter) {
    issues(filter: $filter, first: 1) { totalCount }
  }
`)
