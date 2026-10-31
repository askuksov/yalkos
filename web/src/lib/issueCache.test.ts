import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import { issueRow } from '../test/fixtures'
import { patchIssue } from './issueCache'

describe('issueCache', () => {
  it('patches the node in all caches and rolls back', () => {
    const qc = new QueryClient()
    const a = issueRow(1)
    const b = issueRow(2)
    qc.setQueryData(['issues', 'YLK', 's-todo', ''], { pages: [{ issues: { totalCount: 2, nodes: [a, b], pageInfo: {} } }], pageParams: [null] })
    qc.setQueryData(['children', 'i9', 'SORT_ORDER'], { issue: { id: 'i9', children: { nodes: [a] } } })
    qc.setQueryData(['issue', 'YLK-1'], { issue: { ...a, description: 'x' } })
    qc.setQueryData(['issue', 'YLK-2'], { issue: { ...b, description: 'y' } })

    const rollback = patchIssue(qc, 'i1', { priority: 1, title: 'Renamed' })

    type Pages = { pages: { issues: { nodes: { id: string; priority: number; title: string }[] } }[] }
    const list = qc.getQueryData<Pages>(['issues', 'YLK', 's-todo', ''])
    expect(list?.pages[0].issues.nodes[0]).toMatchObject({ priority: 1, title: 'Renamed' })
    expect(list?.pages[0].issues.nodes[1]).toBe(b)
    expect(qc.getQueryData<{ issue: { children: { nodes: { priority: number }[] } } }>(['children', 'i9', 'SORT_ORDER'])?.issue.children.nodes[0].priority).toBe(1)
    expect(qc.getQueryData<{ issue: { priority: number; description: string } }>(['issue', 'YLK-1'])?.issue).toMatchObject({ priority: 1, description: 'x' })
    expect(qc.getQueryData<{ issue: { priority: number } }>(['issue', 'YLK-2'])?.issue.priority).toBe(0)

    rollback()
    expect(qc.getQueryData<Pages>(['issues', 'YLK', 's-todo', ''])?.pages[0].issues.nodes[0]).toEqual(a)
    expect(qc.getQueryData<{ issue: { priority: number } }>(['issue', 'YLK-1'])?.issue.priority).toBe(0)
  })
})
