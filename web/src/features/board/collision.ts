import { closestCenter, closestCorners, pointerWithin, type CollisionDetection } from '@dnd-kit/core'

import { columnDropId } from './useBoardDrag'

const isColumn = (id: string | number) => String(id).startsWith(columnDropId(''))

// Over a column's header the pointer is above its card list, which is the column droppable.
const columnBelowPointer: CollisionDetection = ({ droppableContainers, droppableRects, pointerCoordinates: p }) => {
  const container =
    p &&
    droppableContainers.find((d) => {
      const r = isColumn(d.id) ? droppableRects.get(d.id) : undefined
      return r && r.left <= p.x && p.x <= r.right && p.y < r.top
    })
  return container ? [{ id: container.id, data: { droppableContainer: container, value: 0 } }] : []
}

/**
 * Follows the pointer: the card under it, else the column under it (or the one whose header it is
 * over). closestCorners alone prefers a same-sized card in the next column over a tall empty column.
 */
export const boardCollision: CollisionDetection = (args) => {
  const hits = pointerWithin(args)
  const card = hits.find((h) => !isColumn(h.id))
  if (card) return [card]
  const column = hits.find((h) => isColumn(h.id)) ?? columnBelowPointer(args)[0]
  if (!column) return closestCorners(args)

  const cards = args.droppableContainers.filter((c) => c.data.current?.sortable?.containerId === column.id)
  if (!cards.length) return [column]
  const lastBottom = Math.max(...cards.map((c) => args.droppableRects.get(c.id)?.bottom ?? -Infinity))
  if (args.pointerCoordinates && args.pointerCoordinates.y > lastBottom) return [column]
  return closestCenter({ ...args, droppableContainers: cards })
}
