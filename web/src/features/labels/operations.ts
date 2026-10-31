import { graphql } from '../../gql'

export const LabelFields = graphql(`
  fragment LabelFields on Label {
    id name color description isGroup exclusive position parent { id }
  }
`)

export const LabelsQuery = graphql(`
  query Labels {
    labels { ...LabelFields children { ...LabelFields } }
    labelSettings { typeGroup { id } defaultType { id } }
  }
`)

export const CreateLabelMutation = graphql(`
  mutation CreateLabel($input: CreateLabelInput!) {
    createLabel(input: $input) { ...LabelFields }
  }
`)

export const UpdateLabelMutation = graphql(`
  mutation UpdateLabel($id: ID!, $input: UpdateLabelInput!) {
    updateLabel(id: $id, input: $input) { ...LabelFields }
  }
`)

export const DeleteLabelMutation = graphql(`
  mutation DeleteLabel($id: ID!) {
    deleteLabel(id: $id)
  }
`)

export const SetDefaultTypeMutation = graphql(`
  mutation SetDefaultType($labelId: ID) {
    setDefaultType(labelId: $labelId) { defaultType { id } }
  }
`)

export const LabelIssueCountQuery = graphql(`
  query LabelIssueCount($id: ID!) {
    issues(filter: { label: { in: [$id] } }, first: 1) { totalCount }
  }
`)
