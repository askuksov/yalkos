import { useDroppable } from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useInfiniteQuery } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { request } from '../../api/client'
import type { IssueFilter } from '../../gql/graphql'
import { formatNumber } from '../../i18n'
import { Button } from '../../ui/Button'
import { StateIcon, type StateTypeName } from '../../ui/StateIcon'
import { IssueGroupQuery } from '../issues/fragments'
import { pageSize } from '../issues/IssueGroup'
import s from './board.module.css'
import { BoardCard } from './BoardCard'
import { boardKey, columnDropId } from './useBoardDrag'

type Props = {
  projectKey: string
  state: { id: string; name: string; type: StateTypeName; color: string }
  filter: IssueFilter
  search: string
  selected: string | null
  onSelect: (id: string) => void
  draggable: boolean
  onCreate?: () => void
}

/** One state: header with count and "+", cards in manual order, more on scroll. */
export function BoardColumn({ projectKey, state, filter, search, selected, onSelect, draggable, onCreate }: Props) {
  const { t } = useTranslation('issues')
  const q = useInfiniteQuery({
    queryKey: boardKey(projectKey, state.id, search),
    queryFn: ({ pageParam }) => request(IssueGroupQuery, { filter, orderBy: { field: 'SORT_ORDER', direction: 'ASC' }, first: pageSize, after: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => (last.issues.pageInfo.hasNextPage ? last.issues.pageInfo.endCursor : null),
  })
  const nodes = q.data?.pages.flatMap((p) => p.issues.nodes) ?? []
  const total = q.data?.pages[0]?.issues.totalCount
  const { setNodeRef, isOver } = useDroppable({ id: columnDropId(state.id), disabled: !draggable })

  // Load the next page when the end of the column scrolls into view.
  const end = useRef<HTMLDivElement>(null)
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q
  useEffect(() => {
    const el = end.current
    if (!el || !hasNextPage || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && !isFetchingNextPage) void fetchNextPage()
    })
    io.observe(el)
    return () => io.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  return (
    <section className={[s.column, isOver ? s.over : ''].join(' ')} data-column={state.id}>
      <header className={s.columnHeader}>
        <StateIcon type={state.type} color={state.color} />
        <span className={s.columnName}>{state.name}</span>
        {total !== undefined && <span className={s.count}>{formatNumber(total)}</span>}
        <span className={s.grow} />
        {onCreate && <Button size="sm" variant="ghost" icon={<Plus size={14} aria-hidden />} aria-label={t('create.inState', { state: state.name })} onClick={onCreate} />}
      </header>
      <SortableContext items={nodes.map((n) => n.id)} strategy={verticalListSortingStrategy}>
        <div ref={setNodeRef} className={s.cards} role="listbox" aria-label={state.name}>
          {nodes.map((n) => (
            <BoardCard key={n.id} issue={n} selected={selected === n.id} onHover={() => onSelect(n.id)} draggable={draggable} />
          ))}
          <div ref={end} className={s.end} />
        </div>
      </SortableContext>
    </section>
  )
}
