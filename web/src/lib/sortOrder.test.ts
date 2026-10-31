import { describe, expect, it } from 'vitest'

import { between, dropOrder } from './sortOrder'

describe('sortOrder', () => {
  it('between handles all neighbour cases', () => {
    expect(between(1, 2)).toBe(1.5)
    expect(between(undefined, 10)).toBe(-990)
    expect(between(10, undefined)).toBe(1010)
    expect(between()).toBe(0)
  })

  it('dropOrder places at top, middle and bottom', () => {
    const list = [100, 200, 300]
    expect(dropOrder(list, 0)).toBe(-900)
    expect(dropOrder(list, 1)).toBe(150)
    expect(dropOrder(list, 3)).toBe(1300)
    expect(dropOrder([], 0)).toBe(0)
    // With the moving item still in the list: 300 moves to the top.
    expect(dropOrder(list, 0, 2)).toBe(-900)
    // 100 moves below 200.
    expect(dropOrder(list, 1, 0)).toBe(250)
  })
})
