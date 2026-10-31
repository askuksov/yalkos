import { graphql } from '../../gql'

export const IssueLinkQuery = graphql(`
  query IssueLink($id: String!) {
    issue(id: $id) { id identifier title state { id name type color } }
  }
`)

export const LimitsQuery = graphql(`
  query Limits {
    limits { maxUploadMb }
  }
`)
