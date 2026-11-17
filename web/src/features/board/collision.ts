import { closestCenter, closestCorners, pointerWithin, type CollisionDetection } from '@dnd-kit/core'

import { columnDropId } from './useBoardDrag'

const isColumn = (id: string | number) => String(id).startsWith(columnDropId(''))

/**
 * Follows the pointer: the card under it, else the column under it. closestCorners alone
 * prefers a same-sized card in the next column over a tall empty column.
 */
export const boardCollision: CollisionDetection = (args) => {
  const hits = pointerWithin(args)
  const card = hits.find((h) => !isColumn(h.id))
  if (card) return [card]
  const column = hits.find((h) => isColumn(h.id))
  if (!column) return closestCorners(args)

  const cards = args.droppableContainers.filter((c) => c.data.current?.sortable?.containerId === column.id)
  const lastBottom = Math.max(...cards.map((c) => args.droppableRects.get(c.id)?.bottom ?? -Infinity))
  const below = (args.pointerCoordinates?.y ?? 0) > lastBottom
  if (!cards.length || below) return [column]
  return closestCenter({ ...args, droppableContainers: cards })
}
