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
const entry = (changes: [string, unknown, unknown][], action = 'update', actor: HistoryEntry['actor'] = olga, names: HistoryEntry['names'] = []): HistoryEntry => ({
  id: 'h', action, actor, createdAt: '2026-10-30T14:02:00Z', names,
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

  group('stored names', () => {
    const live: DescribeContext = {
      ...ctx,
      labels: new Map([['perf', { name: 'Performance', type: false }], ['feat', { name: 'Feature', type: true }]]),
      milestones: new Map([['m1', 'Bank APIs']]),
    }
    const named = (changes: [string, unknown, unknown][], names: HistoryEntry['names']) => describe(entry(changes, 'update', olga, names), live)

    it('describes a deleted type label with its stored name as a type', () => {
      const bug = { id: 'bug', name: 'Bug', type: true }
      expect(named([['labelIds', [], ['bug']]], [bug])).toEqual(['Olga set type to Bug'])
      expect(named([['labelIds', ['bug'], []]], [bug])).toEqual(['Olga removed type Bug'])
      expect(named([['labelIds', ['bug'], ['feat']]], [bug, { id: 'feat', name: 'Feat', type: true }])).toEqual(['Olga changed type from Bug to Feature'])
    })

    it('shows an extra type id as an ordinary label line', () => {
      const bug = { id: 'bug', name: 'Bug', type: true }
      expect(named([['labelIds', [], ['bug', 'feat']]], [bug])).toEqual(['Olga set type to Bug', 'Olga added label Feature'])
      expect(named([['labelIds', ['bug', 'feat'], []]], [bug])).toEqual(['Olga removed type Bug', 'Olga removed label Feature'])
    })

    it('names a deleted ordinary label by its stored name', () => {
      const old = { id: 'old', name: 'Legacy', type: false }
      expect(named([['labelIds', [], ['old']]], [old])).toEqual(['Olga added label Legacy'])
      expect(named([['labelIds', ['old'], []]], [old])).toEqual(['Olga removed label Legacy'])
    })

    it('names a milestone of the previous project by its stored name', () => {
      const q3 = { id: 'm-old', name: 'Q3 release', type: false }
      expect(named([['milestoneId', null, 'm-old']], [q3])).toEqual(['Olga added the issue to milestone Q3 release'])
      expect(named([['milestoneId', 'm-old', null]], [q3])).toEqual(['Olga removed it from milestone Q3 release'])
    })

    it('control: a renamed live label shows its live name and live type flag', () => {
      expect(named([['labelIds', [], ['perf']]], [{ id: 'perf', name: 'Perf', type: true }])).toEqual(['Olga added label Performance'])
      expect(named([['milestoneId', null, 'm1']], [{ id: 'm1', name: 'Banks', type: false }])).toEqual(['Olga added the issue to milestone Bank APIs'])
    })

    it('control: an entry without stored names keeps the generic text', () => {
      expect(named([['labelIds', ['gone'], []]], [])).toEqual(['Olga removed label a deleted label'])
      expect(named([['milestoneId', 'm-old', null]], [])).toEqual(['Olga removed it from milestone a deleted milestone'])
    })
  })
})
