import { closestCenter, closestCorners, type CollisionDetection } from '@dnd-kit/core'
import { describe, expect, it } from 'vitest'

import { boardCollision } from './collision'
import { columnDropId } from './useBoardDrag'

type Args = Parameters<CollisionDetection>[0]
type Box = { left: number; top: number; width: number; height: number }

const rect = ({ left, top, width, height }: Box) => ({ left, top, width, height, right: left + width, bottom: top + height })

// Two columns 200 wide: "a" (empty unless cards are given) and "b" (one card at the top), 900 tall.
const colA: Box = { left: 0, top: 0, width: 200, height: 900 }
const colB: Box = { left: 210, top: 0, width: 200, height: 900 }
const cardSize = { width: 200, height: 80 }

function layout(cards: { id: string; column: 'a' | 'b'; top: number }[], pointer: { x: number; y: number } | null, dragged: Box = { left: 0, top: 60, ...cardSize }): Args {
  const entries = [
    { id: columnDropId('a'), box: colA, data: undefined as unknown },
    { id: columnDropId('b'), box: colB, data: undefined as unknown },
    ...cards.map((c) => ({
      id: c.id,
      box: { left: c.column === 'a' ? colA.left : colB.left, top: c.top, ...cardSize },
      data: { sortable: { containerId: columnDropId(c.column) } },
    })),
  ]
  return {
    active: { id: 'dragged' },
    collisionRect: rect(dragged),
    droppableRects: new Map(entries.map((e) => [e.id, rect(e.box)])),
    droppableContainers: entries.map((e) => ({ id: e.id, data: { current: e.data } })),
    pointerCoordinates: pointer,
  } as unknown as Args
}

const ids = (c: ReturnType<CollisionDetection>) => c.map((x) => x.id)

describe('boardCollision', () => {
  const nextColumnCard = { id: 'b1', column: 'b' as const, top: 0 }

  it('control: closestCorners prefers the neighbour card over a tall empty column', () => {
    const args = layout([nextColumnCard], { x: 100, y: 100 })
    expect(ids(closestCorners(args))[0]).toBe('b1')
  })

  it('drops into the empty column under the pointer', () => {
    const args = layout([nextColumnCard], { x: 100, y: 100 })
    expect(ids(boardCollision(args))).toEqual([columnDropId('a')])
  })

  it('returns the card under the pointer', () => {
    const cards = [{ id: 'a1', column: 'a' as const, top: 0 }, { id: 'a2', column: 'a' as const, top: 90 }, nextColumnCard]
    expect(ids(boardCollision(layout(cards, { x: 100, y: 120 })))).toEqual(['a2'])
  })

  it('picks the nearer card of the column under the pointer, not of the neighbour', () => {
    // Pointer in the gap between a1 and a2; b1 sits at the same height and is closer to the dragged rect.
    const cards = [{ id: 'a1', column: 'a' as const, top: 0 }, { id: 'a2', column: 'a' as const, top: 200 }, { id: 'b1', column: 'b' as const, top: 90 }]
    const dragged: Box = { left: 100, top: 80, ...cardSize }
    const args = layout(cards, { x: 150, y: 130 }, dragged)
    expect(ids(closestCenter(args))[0]).toBe('b1')
    expect(ids(boardCollision(args))).toEqual(['a1', 'a2'])
  })

  it('returns the column when the pointer is below its last card', () => {
    const cards = [{ id: 'a1', column: 'a' as const, top: 0 }, nextColumnCard]
    expect(ids(boardCollision(layout(cards, { x: 100, y: 500 })))).toEqual([columnDropId('a')])
  })

  it('drops into the empty column whose header is under the pointer', () => {
    // The card list starts below the header: the pointer is above column a's list.
    expect(ids(boardCollision(layout([nextColumnCard], { x: 100, y: -10 })))).toEqual([columnDropId('a')])
  })

  it('picks the top card of the non-empty column whose header is under the pointer', () => {
    const cards = [{ id: 'a1', column: 'a' as const, top: 0 }, { id: 'a2', column: 'a' as const, top: 90 }, nextColumnCard]
    // The dragged rect leans towards column b, so closestCorners alone would pick b1.
    const args = layout(cards, { x: 100, y: -10 }, { left: 150, top: -50, ...cardSize })
    expect(ids(closestCorners(args))[0]).toBe('b1')
    expect(ids(boardCollision(args))[0]).toBe('a1')
  })

  it('falls back to closestCorners in the gap between columns', () => {
    const args = layout([nextColumnCard], { x: 205, y: 100 })
    expect(ids(boardCollision(args))).toEqual(ids(closestCorners(args)))
  })

  it('falls back to closestCorners without a pointer (keyboard drags)', () => {
    const args = layout([nextColumnCard], null)
    expect(boardCollision(args)).toEqual(closestCorners(args))
  })
})
