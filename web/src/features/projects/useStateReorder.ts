import type { DragEndEvent } from '@dnd-kit/core'
import { arrayMove } from '@dnd-kit/sortable'
import { useMutation } from '@tanstack/react-query'

import { errorMessage, request } from '../../api/client'
import { useToast } from '../../ui/useToast'
import { ReorderStatesMutation } from './operations'
import { useInvalidateProjects } from './useInvalidateProjects'

/** Drag end on the states list → reorderStates with the new id order. */
export function useStateReorder(projectId: string, stateIds: string[]) {
  const toast = useToast()
  const invalidate = useInvalidateProjects()
  const reorder = useMutation({
    mutationFn: (ids: string[]) => request(ReorderStatesMutation, { projectId, stateIds: ids }),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => invalidate(),
  })
  const onDragEnd = (e: DragEndEvent) => {
    const from = stateIds.indexOf(String(e.active.id))
    const to = e.over ? stateIds.indexOf(String(e.over.id)) : -1
    if (from < 0 || to < 0 || from === to) return
    reorder.mutate(arrayMove(stateIds, from, to))
  }
  return { onDragEnd }
}
