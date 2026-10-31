import { useInfiniteQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { type ReactNode, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { request } from '../../api/client'
import type { IssueFilter, IssueOrder } from '../../gql/graphql'
import { formatNumber } from '../../i18n'
import { Button } from '../../ui/Button'
import { IssueGroupQuery } from './fragments'
import { IssueRow } from './IssueRow'
import s from './issues.module.css'

export const pageSize = 50

/** Collapse toggles the user made, kept while the app is open. */
const collapsedMemory = new Map<string, boolean>()

type Props = {
  /** Query key parts: scope ('my' or project key), group id, URL search. */
  scope: string
  groupId: string
  search: string
  label: string
  icon: ReactNode
  filter: IssueFilter
  orderBy: IssueOrder
  nested: boolean
  /** Starts collapsed for this total (Canceled always, Done above 10). */
  collapseFor: (total: number) => boolean
  /** Hide the group when it has no issues (filters active). */
  hideEmpty: boolean
  onCount: (groupId: string, total: number) => void
  /** Header action, e.g. "+" to create in this state. */
  action?: ReactNode
}

/** One state (or state type) group: its own paginated query, header with count. */
export function IssueGroup({ scope, groupId, search, label, icon, filter, orderBy, nested, collapseFor, hideEmpty, onCount, action }: Props) {
  const { t } = useTranslation('issues')
  const q = useInfiniteQuery({
    queryKey: ['issues', scope, groupId, search],
    queryFn: ({ pageParam }) => request(IssueGroupQuery, { filter, orderBy, first: pageSize, after: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => (last.issues.pageInfo.hasNextPage ? last.issues.pageInfo.endCursor : null),
  })
  const total = q.data?.pages[0]?.issues.totalCount
  useEffect(() => {
    if (total !== undefined) onCount(groupId, total)
  }, [total, groupId, onCount])

  const memoryKey = `${scope}:${groupId}`
  const [toggled, setToggled] = useState(collapsedMemory.get(memoryKey))
  if (total === undefined) return null
  if (total === 0 && hideEmpty) return null
  const collapsed = toggled ?? collapseFor(total)
  const nodes = q.data?.pages.flatMap((p) => p.issues.nodes) ?? []

  return (
    <section className={s.group}>
      <div className={s.groupHeader}>
        <button
          type="button"
          className={s.groupToggle}
          aria-expanded={!collapsed}
          onClick={() => {
            collapsedMemory.set(memoryKey, !collapsed)
            setToggled(!collapsed)
          }}
        >
          {collapsed ? <ChevronRight size={14} aria-hidden /> : <ChevronDown size={14} aria-hidden />}
          {icon}
          <span>{label}</span>{' '}
          <span className={s.count}>{formatNumber(total)}</span>
        </button>
        {action}
      </div>
      {!collapsed && (
        <div role="listbox" aria-label={label} aria-multiselectable>
          {nodes.map((i) => (
            <IssueRow key={i.id} issue={i} nested={nested} orderBy={orderBy} />
          ))}
          {q.hasNextPage && (
            <div className={s.more}>
              <Button size="sm" variant="ghost" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {t('list.showMore')}
              </Button>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
