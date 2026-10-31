import { graphql } from '../../gql'

export const MilestoneFields = graphql(`
  fragment MilestoneFields on Milestone {
    id name targetDate status statusAutomatic overdue
    progress { completed total }
    lead { id name }
    project { id key }
  }
`)

export const MilestonesQuery = graphql(`
  query Milestones($projectId: String!) {
    project(id: $projectId) { id milestones { ...MilestoneFields } }
  }
`)

export const MilestonePageQuery = graphql(`
  query MilestonePage($id: ID!) {
    milestone(id: $id) { ...MilestoneFields description createdBy { id } }
  }
`)

export const CreateMilestoneMutation = graphql(`
  mutation CreateMilestone($projectId: ID!, $input: CreateMilestoneInput!) {
    createMilestone(projectId: $projectId, input: $input) { id }
  }
`)

export const UpdateMilestoneMutation = graphql(`
  mutation UpdateMilestone($id: ID!, $input: UpdateMilestoneInput!) {
    updateMilestone(id: $id, input: $input) { ...MilestoneFields description }
  }
`)

export const ReorderMilestonesMutation = graphql(`
  mutation ReorderMilestones($projectId: ID!, $milestoneIds: [ID!]!) {
    reorderMilestones(projectId: $projectId, milestoneIds: $milestoneIds) { id }
  }
`)

export const DeleteMilestoneMutation = graphql(`
  mutation DeleteMilestone($id: ID!) {
    deleteMilestone(id: $id)
  }
`)
