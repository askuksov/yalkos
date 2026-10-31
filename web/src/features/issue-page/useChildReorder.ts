import type { DragEndEvent } from '@dnd-kit/core'
import { arrayMove } from '@dnd-kit/sortable'
import { useQueryClient } from '@tanstack/react-query'

import type { IssueChildrenQuery as ChildrenData } from '../../gql/graphql'
import { useIssueMutation } from '../../lib/issueCache'
import { dropOrder } from '../../lib/sortOrder'

export const childOrder = { field: 'SORT_ORDER', direction: 'ASC' } as const

export const childrenKey = (issueId: string) => ['children', issueId, childOrder.field, childOrder.direction]

/** Drag end on the sub-issue list: moves the row at once, saves sortOrder, restores the order on error. */
export function useChildReorder(issueId: string) {
  const qc = useQueryClient()
  const update = useIssueMutation()
  const onDragEnd = (e: DragEndEvent) => {
    const key = childrenKey(issueId)
    const before = qc.getQueryData<ChildrenData>(key)
    const nodes = before?.issue.children.nodes ?? []
    const from = nodes.findIndex((n) => n.id === e.active.id)
    const to = nodes.findIndex((n) => n.id === e.over?.id)
    if (!before || from < 0 || to < 0 || from === to) return
    void qc.cancelQueries({ queryKey: key })
    const sortOrder = dropOrder(nodes.map((n) => n.sortOrder), to, from)
    qc.setQueryData<ChildrenData>(key, { issue: { ...before.issue, children: { nodes: arrayMove(nodes, from, to) } } })
    update.mutate({ id: nodes[from].id, input: { sortOrder }, optimistic: { sortOrder } }, { onError: () => qc.setQueryData(key, before) })
  }
  return { onDragEnd }
}
