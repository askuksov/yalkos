import { useEffect, useState } from 'react'

import { graphql } from '../../gql'

export const PaletteSearchQuery = graphql(`
  query PaletteSearch($filter: IssueFilter, $first: Int) {
    issues(filter: $filter, first: $first, orderBy: { field: UPDATED_AT, direction: DESC }) {
      nodes { id identifier title project { id key } state { name type color } }
    }
  }
`)

export const PaletteChildCountQuery = graphql(`
  query PaletteChildCount($id: String!) {
    issue(id: $id) { id children(first: 1) { totalCount } milestone { id } }
  }
`)

export const PaletteMilestonesQuery = graphql(`
  query PaletteMilestones {
    projects(first: 100) { nodes { id key isMember milestones(includeDone: false) { id name } } }
  }
`)

/** Debounced value (as-you-type search). */
export function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return v
}
