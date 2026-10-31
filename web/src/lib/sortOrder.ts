// Manual order: a dropped issue gets a sortOrder between its new neighbours.

const gap = 1000

/** Midpoint of two neighbours; 1000 above the first or below the last; 0 alone. */
export function between(prev?: number, next?: number): number {
  if (prev !== undefined && next !== undefined) return (prev + next) / 2
  if (next !== undefined) return next - gap
  if (prev !== undefined) return prev + gap
  return 0
}

/**
 * sortOrder for a drop at toIndex. list holds the orders in display order
 * without the moving item; with movingFrom it still contains it at that index.
 */
export function dropOrder(list: number[], toIndex: number, movingFrom?: number): number {
  const rest = movingFrom === undefined ? list : list.filter((_, i) => i !== movingFrom)
  const at = Math.min(Math.max(toIndex, 0), rest.length)
  return between(rest[at - 1], rest[at])
}
