import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { request } from '../../api/client'
import { graphql } from '../../gql'
import { Picker } from '../../ui/Picker'

const PickIssuesQuery = graphql(`
  query PickIssues($filter: IssueFilter) {
    issues(filter: $filter, first: 20, orderBy: { field: UPDATED_AT, direction: DESC }) {
      nodes { id identifier title }
    }
  }
`)

export type PickerIssue = { id: string; identifier: string; title: string }

type Props = {
  projectId: string
  /** The issue being edited: never offered. */
  excludeId?: string
  value: string | null
  onSelect: (issue: PickerIssue) => void
  onClose: () => void
  placeholder?: string
}

/** Issues of one project, searched on the server as you type. */
export function IssuePicker({ projectId, excludeId, value, onSelect, onClose, placeholder }: Props) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const [search, setSearch] = useState('')
  useEffect(() => {
    const id = setTimeout(() => setSearch(q.trim()), 150)
    return () => clearTimeout(id)
  }, [q])
  const issues = useQuery({
    queryKey: ['pickIssues', projectId, search],
    queryFn: () => request(PickIssuesQuery, { filter: { project: { eq: projectId }, ...(search ? { search } : {}) } }),
    placeholderData: keepPreviousData,
  })
  const items = (issues.data?.issues.nodes ?? []).filter((i) => i.id !== excludeId)
  return (
    <Picker
      items={items}
      value={items.find((i) => i.id === value) ?? null}
      getKey={(i) => i.id}
      getLabel={(i) => `${i.identifier} ${i.title}`}
      renderItem={(i) => (
        <>
          <span style={{ color: 'var(--fg-muted)', minInlineSize: 56 }}>{i.identifier}</span>
          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{i.title}</span>
        </>
      )}
      onSelect={onSelect}
      onClose={onClose}
      onQuery={setQ}
      placeholder={placeholder ?? t('pickers.issue')}
    />
  )
}
