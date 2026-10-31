import type { DragEndEvent } from '@dnd-kit/core'
import { arrayMove } from '@dnd-kit/sortable'
import { useMutation, useQueryClient } from '@tanstack/react-query'

import { errorMessage, request } from '../../api/client'
import { useToast } from '../../ui/useToast'
import { ReorderMilestonesMutation } from './operations'

/** Drag end on the milestones list → reorderMilestones with the new id order. */
export function useMilestoneReorder(projectId: string, projectKey: string, ids: string[]) {
  const qc = useQueryClient()
  const toast = useToast()
  const reorder = useMutation({
    mutationFn: (milestoneIds: string[]) => request(ReorderMilestonesMutation, { projectId, milestoneIds }),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['milestones', projectKey] }),
  })
  const onDragEnd = (e: DragEndEvent) => {
    const from = ids.indexOf(String(e.active.id))
    const to = e.over ? ids.indexOf(String(e.over.id)) : -1
    if (from < 0 || to < 0 || from === to) return
    reorder.mutate(arrayMove(ids, from, to))
  }
  return { onDragEnd }
}
