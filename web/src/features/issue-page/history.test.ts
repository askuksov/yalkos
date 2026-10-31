import { describe as group, expect, it } from 'vitest'

import { i18n } from '../../i18n'
import { describe, type DescribeContext, type HistoryEntry, numbersBefore } from './history'

const olga = { id: 'u1', name: 'Olga' }
const ctx: DescribeContext = {
  t: i18n.getFixedT(null, ['issue', 'common']),
  viewerId: 'u1',
  users: new Map([['u1', 'Olga'], ['u2', 'Andrii']]),
  states: new Map([['s-todo', 'Todo'], ['s-done', 'Done']]),
  issues: new Map([['i9', 'YLK-9']]),
  projects: new Map([['p1', 'YLK'], ['p2', 'WEB']]),
}
const j = (v: unknown) => (v === null ? null : JSON.stringify(v))
const entry = (changes: [string, unknown, unknown][], action = 'update', actor: HistoryEntry['actor'] = olga): HistoryEntry => ({
  id: 'h', action, actor, createdAt: '2026-10-30T14:02:00Z',
  changes: changes.map(([field, from, to]) => ({ field, from: j(from), to: j(to) })),
})

group('history', () => {
  it('describes each change type', () => {
    const cases: [HistoryEntry, string[]][] = [
      [entry([['title', null, 'Fix']], 'create'), ['Olga created the issue']],
      [entry([['title', 'Old', 'Fix login']]), ['Olga renamed the issue to "Fix login"']],
      [entry([['description', 'a', 'b']]), ['Olga updated the description']],
      [entry([['stateId', 's-todo', 's-done']]), ['Olga changed state from Todo to Done']],
      [entry([['stateId', 's-gone', 's-done']]), ['Olga changed state from a deleted state to Done']],
      [entry([['priority', 0, 2]]), ['Olga set priority to High']],
      [entry([['priority', 2, 0]]), ['Olga removed priority']],
      [entry([['assigneeId', null, 'u2']]), ['Olga assigned Andrii']],
      [entry([['assigneeId', 'u2', null]]), ['Olga unassigned Andrii']],
      [entry([['assigneeId', null, 'u1']]), ['Olga assigned themselves']],
      [entry([['assigneeId', null, 'u7']]), ['Olga assigned someone']],
      [entry([['dueDate', null, '2026-11-04']]), ['Olga set due date to 2026-11-04']],
      [entry([['dueDate', '2026-11-04', null]]), ['Olga removed the due date']],
      [entry([['parentId', null, 'i9']]), ['Olga made this a sub-issue of YLK-9']],
      [entry([['parentId', 'i9', null]]), ['Olga removed the parent']],
      [entry([['title', 'a', 'b']], 'update', null), ['someone renamed the issue to "b"']],
      [entry([['stateId', 's-todo', 's-done'], ['completedAt', null, '2026-10-30T14:02:00Z'], ['priority', 0, 1]]), ['Olga changed state from Todo to Done', 'Olga set priority to Urgent']],
    ]
    for (const [e, want] of cases) expect(describe(e, ctx)).toEqual(want)
  })

  it('describes a move with the old identifier', () => {
    const e = entry([['projectId', 'p1', 'p2'], ['number', 12, 3], ['stateId', 's-todo', 's-x']])
    expect(describe(e, ctx)).toEqual(['Olga moved the issue from YLK to WEB (was YLK-12)'])
  })

  it('names the number when a move keeps it', () => {
    // The audit diff leaves out an unchanged number; the page supplies it.
    const e = entry([['projectId', 'p1', 'p2'], ['stateId', 's-todo', 's-x']])
    expect(describe(e, { ...ctx, numberAt: new Map([['h', 1]]) })).toEqual(['Olga moved the issue from YLK to WEB (was YLK-1)'])
    // Walking back from the current number 3: the second move changed 12 → 3.
    const moves = [
      { ...entry([['projectId', 'p1', 'p2']]), id: 'm1' },
      { ...entry([['projectId', 'p2', 'p1'], ['number', 12, 3]]), id: 'm2' },
    ]
    expect([...numbersBefore(moves, 3)]).toEqual([['m2', 12], ['m1', 12]])
  })

  it('hides sortOrder, number, completedAt on their own', () => {
    expect(describe(entry([['sortOrder', 1, 2]]), ctx)).toEqual([])
    expect(describe(entry([['number', 1, 2]]), ctx)).toEqual([])
    expect(describe(entry([['completedAt', null, 'x'], ['canceledAt', 'y', null]]), ctx)).toEqual([])
  })
})
