import type { DragEndEvent } from '@dnd-kit/core'
import { type InfiniteData, useMutation, useQueryClient } from '@tanstack/react-query'

import { errorMessage, request } from '../../api/client'
import type { IssueGroupQuery, IssueRowFieldsFragment, UpdateIssueInput } from '../../gql/graphql'
import { dropOrder } from '../../lib/sortOrder'
import { useToast } from '../../ui/useToast'
import { UpdateIssueMutation } from '../issues/fragments'

type Pages = InfiniteData<IssueGroupQuery, string | null>

/** Query key of a board column (under 'issues', so list-wide invalidation reaches it). */
export function boardKey(projectKey: string, stateId: string, search: string) {
  return ['issues', 'board', projectKey, stateId, search] as const
}

export const columnDropId = (stateId: string) => `column:${stateId}`

function nodesOf(d: Pages | undefined): IssueRowFieldsFragment[] {
  return d?.pages.flatMap((p) => p.issues.nodes) ?? []
}

/** Writes nodes back into the pages, keeping each page's length (the last page absorbs the difference). */
function withNodes(d: Pages, nodes: IssueRowFieldsFragment[], countDelta = 0): Pages {
  let at = 0
  const pages = d.pages.map((p, i) => {
    const take = i === d.pages.length - 1 ? nodes.length - at : p.issues.nodes.length
    const slice = nodes.slice(at, at + take)
    at += take
    return { issues: { ...p.issues, nodes: slice, totalCount: p.issues.totalCount + (i === 0 ? countDelta : 0) } }
  })
  return { ...d, pages }
}

type Drop = { id: string; input: UpdateIssueInput; snapshot: { key: readonly unknown[]; data: Pages | undefined }[]; keys: (readonly unknown[])[] }

/**
 * Board drop handler: computes the target column and index, moves the card in
 * the cached columns at once, then saves stateId/sortOrder; on error both
 * columns are restored and an error toast is shown.
 */
export function useBoardDrag(projectKey: string, search: string) {
  const qc = useQueryClient()
  const toast = useToast()
  const save = useMutation({
    mutationFn: (d: Drop) => request(UpdateIssueMutation, { id: d.id, input: d.input }),
    onError: (err, d) => {
      d.snapshot.forEach((s) => qc.setQueryData(s.key, s.data))
      toast.show(errorMessage(err), { kind: 'error' })
    },
    onSettled: (_r, _e, d) => Promise.all(d.keys.map((key) => qc.invalidateQueries({ queryKey: key }))),
  })

  const columnOf = (issueId: string) => {
    for (const q of qc.getQueryCache().findAll({ queryKey: ['issues', 'board', projectKey] })) {
      if (q.queryKey[4] !== search) continue
      const nodes = nodesOf(q.state.data as Pages | undefined)
      const index = nodes.findIndex((n) => n.id === issueId)
      if (index >= 0) return { stateId: String(q.queryKey[3]), nodes, index }
    }
    return null
  }

  const onDragEnd = (e: DragEndEvent) => {
    const activeId = String(e.active.id)
    const overId = e.over ? String(e.over.id) : null
    if (!overId || overId === activeId) return
    const from = columnOf(activeId)
    if (!from) return
    let toState: string
    let toNodes: IssueRowFieldsFragment[]
    let toIndex: number
    if (overId.startsWith('column:')) {
      toState = overId.slice('column:'.length)
      toNodes = nodesOf(qc.getQueryData<Pages>(boardKey(projectKey, toState, search)))
      toIndex = toNodes.filter((n) => n.id !== activeId).length
    } else {
      const over = columnOf(overId)
      if (!over) return
      toState = over.stateId
      toNodes = over.nodes
      toIndex = over.index
    }
    const same = toState === from.stateId
    const sortOrder = same ? dropOrder(from.nodes.map((n) => n.sortOrder), toIndex, from.index) : dropOrder(toNodes.map((n) => n.sortOrder), toIndex)
    if (same && toIndex === from.index) return

    const fromKey = boardKey(projectKey, from.stateId, search)
    const toKey = boardKey(projectKey, toState, search)
    // A column refetch already in flight would land without this drop.
    void qc.cancelQueries({ queryKey: fromKey })
    void qc.cancelQueries({ queryKey: toKey })
    const snapshot = [
      { key: fromKey, data: qc.getQueryData<Pages>(fromKey) },
      ...(same ? [] : [{ key: toKey, data: qc.getQueryData<Pages>(toKey) }]),
    ]
    const moving = from.nodes[from.index]
    const target = toNodes.length ? toNodes : []
    const card: IssueRowFieldsFragment = { ...moving, sortOrder }
    if (!same) {
      const st = target[0]?.state
      const known = qc
        .getQueryCache()
        .findAll({ queryKey: ['project'] })
        .flatMap((q) => (q.state.data as { project?: { states?: IssueRowFieldsFragment['state'][] } } | undefined)?.project?.states ?? [])
        .find((x) => x.id === toState)
      card.state = known ? { id: known.id, name: known.name, type: known.type, color: known.color } : (st ?? moving.state)
    }
    if (same) {
      const rest = from.nodes.filter((n) => n.id !== activeId)
      rest.splice(toIndex, 0, card)
      qc.setQueryData<Pages>(fromKey, (d) => d && withNodes(d, rest))
    } else {
      qc.setQueryData<Pages>(fromKey, (d) => d && withNodes(d, from.nodes.filter((n) => n.id !== activeId), -1))
      const next = [...toNodes]
      next.splice(toIndex, 0, card)
      qc.setQueryData<Pages>(toKey, (d) => (d ? withNodes(d, next, 1) : { pages: [{ issues: { totalCount: 1, nodes: next, pageInfo: { hasNextPage: false, endCursor: null } } }], pageParams: [null] }))
    }
    save.mutate({ id: activeId, input: same ? { sortOrder } : { stateId: toState, sortOrder }, snapshot, keys: same ? [fromKey] : [fromKey, toKey] })
  }

  return { onDragEnd }
}
