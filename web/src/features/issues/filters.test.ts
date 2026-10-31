import { describe, expect, it } from 'vitest'

import { addDays, parseParams, toFilter, toSearch } from './filters'

const sp = (s: string) => new URLSearchParams(s)

describe('filters', () => {
  it('round-trips params', () => {
    const full = 'state=s1,s2&type=started,unstarted&assignee=me,u1&priority=1,2&due=overdue&parent=none&children=1&q=fix%20login&order=priority&dir=desc&nest=0'
    expect(toSearch(parseParams(sp(full)))).toBe(full)
    const date = 'due=2026-11-04'
    expect(toSearch(parseParams(sp(date)))).toBe(date)
  })

  it('drops invalid values', () => {
    const p = parseParams(sp('priority=9,a&type=done&due=yesterday&order=size&dir=up&parent=some&children=yes'))
    expect(p.priority).toEqual([])
    expect(p.type).toEqual([])
    expect(p.due).toBeUndefined()
    expect(p.parent).toBeUndefined()
    expect(p.children).toBeUndefined()
    expect(toSearch(p)).toBe('')
    expect(parseParams(sp('due=2026-02-30')).due).toBeUndefined()
  })

  it('maps assignee me and none', () => {
    const today = '2026-10-30'
    expect(toFilter(parseParams(sp('assignee=me')), today).assignee).toEqual({ isMe: true })
    expect(toFilter(parseParams(sp('assignee=none')), today).assignee).toEqual({ null: true })
    expect(toFilter(parseParams(sp('assignee=u1,u2')), today).assignee).toEqual({ in: ['u1', 'u2'] })
    expect(parseParams(sp('assignee=none,u1')).assignee).toEqual(['none'])
    // Me together with people needs the viewer id (one "in" list).
    expect(toFilter(parseParams(sp('assignee=me,u2')), today, 'u1').assignee).toEqual({ in: ['u1', 'u2'] })
  })

  it('maps due presets with the given today', () => {
    const today = '2026-10-30'
    expect(toFilter(parseParams(sp('due=week')), today).dueDate).toEqual({ gte: '2026-10-30', lte: '2026-11-05' })
    const overdue = toFilter(parseParams(sp('due=overdue')), today)
    expect(overdue.dueDate).toEqual({ lt: '2026-10-30' })
    expect(overdue.stateType).toEqual({ nin: ['COMPLETED', 'CANCELED'] })
    expect(toFilter(parseParams(sp('due=none')), today).dueDate).toEqual({ null: true })
    expect(toFilter(parseParams(sp('due=2026-12-01')), today).dueDate).toEqual({ lt: '2026-12-01' })
    expect(addDays('2026-12-30', 6)).toBe('2027-01-05')
  })

  it('maps the remaining conditions', () => {
    const f = toFilter(parseParams(sp('state=s1&type=started&priority=1,0&parent=none&children=1&q=%20login%20')), '2026-10-30')
    expect(f).toEqual({
      state: { in: ['s1'] },
      stateType: { in: ['STARTED'] },
      priority: { in: [1, 0] },
      parent: { null: true },
      hasChildren: true,
      search: 'login',
    })
  })

  it('omits defaults from the URL', () => {
    const p = parseParams(sp(''))
    expect(p).toMatchObject({ order: 'SORT_ORDER', dir: 'ASC', nest: true, view: 'list', state: [], q: undefined })
    expect(toSearch(p)).toBe('')
    expect(parseParams(sp(''), 'board').view).toBe('board')
  })
})
