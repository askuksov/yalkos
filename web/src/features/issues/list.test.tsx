import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { conn, issueRow, owner, project, signedInAs, stateOf } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, mocked, serve } from '../../test/serve'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

type Vars = Record<string, unknown> | undefined
const stateEq = (v: Vars) => ((v?.filter as { state?: { eq?: string } })?.state?.eq ?? '') as string

/** Groups by state id → rows; every other state is empty. */
function withGroups(rows: Record<string, ReturnType<typeof issueRow>[]>, totals: Record<string, number> = {}, extra = {}) {
  return {
    ...signedInAs(owner),
    IssueCount: () => ({ issues: { totalCount: Object.values(rows).flat().length } }),
    IssueGroup: (v: Vars) => conn(rows[stateEq(v)] ?? [], { total: totals[stateEq(v)] }),
    ...extra,
  }
}

const todo = (n: number, over = {}) => issueRow(n, { state: stateOf('s-todo'), ...over })

describe('issue list', () => {
  it('renders one group per state with counts', async () => {
    serve(withGroups({ 's-todo': [todo(1), todo(2)] }))
    const { unmount } = renderAt('/p/YLK')
    expect(await screen.findByRole('button', { name: 'Todo 2' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: 'Done 0' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Issue 1/ })).toHaveAttribute('href', '/issue/YLK-1')
    unmount()

    renderAt('/p/YLK?priority=0')
    expect(await screen.findByRole('button', { name: 'Todo 2' })).toBeInTheDocument()
    await waitFor(() => expect(callsOf('IssueGroup').length).toBeGreaterThanOrEqual(10))
    expect(screen.queryByRole('button', { name: 'Done 0' })).not.toBeInTheDocument()
  })

  it('shows issues created while the project was empty', async () => {
    let rows: ReturnType<typeof issueRow>[] = []
    serve({ ...withGroups({}), IssueGroup: (v: Vars) => conn(stateEq(v) === 's-todo' ? rows : []) })
    const { client } = renderAt('/p/YLK')
    expect(await screen.findByText('No issues yet')).toBeInTheDocument()
    rows = [todo(1)]
    await client.invalidateQueries({ queryKey: ['issues'] })
    expect(await screen.findByText('Issue 1')).toBeInTheDocument()
    expect(screen.queryByText('No issues yet')).not.toBeInTheDocument()
  })

  it('collapses Canceled and large Done by default', async () => {
    const done = Array.from({ length: 11 }, (_, i) => issueRow(20 + i, { state: stateOf('s-done') }))
    const canceled = [issueRow(40, { state: stateOf('s-canceled') })]
    serve(withGroups({ 's-todo': [todo(1)], 's-done': done, 's-canceled': canceled }))
    renderAt('/p/YLK')
    expect(await screen.findByRole('button', { name: 'Todo 1' })).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByRole('button', { name: 'Done 11' })).toHaveAttribute('aria-expanded', 'false')
    expect(await screen.findByRole('button', { name: 'Canceled 1' })).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Issue 40')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Canceled 1' }))
    expect(await screen.findByText('Issue 40')).toBeInTheDocument()
  })

  it('loads more with Show more', async () => {
    serve({
      ...withGroups({}),
      IssueGroup: (v: Vars) => {
        if (stateEq(v) !== 's-todo') return conn([])
        return v?.after === 'c1' ? conn([todo(3)], { total: 3 }) : conn([todo(1), todo(2)], { total: 3, next: 'c1' })
      },
    })
    renderAt('/p/YLK')
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }))
    expect(await screen.findByText('Issue 3')).toBeInTheDocument()
    expect(callsOf('IssueGroup').some((v) => v.after === 'c1' && v.first === 50)).toBe(true)
  })

  it('expands sub-issues in nested mode', async () => {
    const parent = todo(1, { subIssueProgress: { completed: 1, total: 2 } })
    serve(
      withGroups({ 's-todo': [parent] }, {}, {
        IssueChildren: (v: Vars) => {
          expect(v?.id).toBe('i1')
          return { issue: { id: 'i1', children: { nodes: [todo(5, { parent: { id: 'i1', identifier: 'YLK-1' } }), issueRow(6, { state: stateOf('s-done') })] } } }
        },
      }),
    )
    renderAt('/p/YLK')
    expect(await screen.findByText('1/2')).toBeInTheDocument()
    // Nested (default) lists top-level issues only.
    expect(callsOf('IssueGroup').every((v) => (v.filter as { parent?: unknown }).parent !== undefined)).toBe(true)
    await userEvent.click(screen.getByRole('button', { name: 'Show sub-issues of YLK-1' }))
    expect(await screen.findByText('Issue 5')).toBeInTheDocument()
    expect(screen.getByText('Issue 6')).toBeInTheDocument()
  })

  it('moves selection with J and K and opens with Enter', async () => {
    serve(withGroups({ 's-todo': [todo(1), todo(2)] }))
    renderAt('/p/YLK')
    await screen.findByText('Issue 2')
    fireEvent.keyDown(document.body, { key: 'j', code: 'KeyJ' })
    fireEvent.keyDown(document.body, { key: 'j', code: 'KeyJ' })
    fireEvent.keyDown(document.body, { key: 'k', code: 'KeyK' })
    fireEvent.keyDown(document.body, { key: 'j', code: 'KeyJ' })
    expect(screen.getByRole('link', { name: /Issue 2/ }).closest('[data-row-id]')).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(document.body, { key: 'Enter', code: 'Enter' })
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/issue/YLK-2'))
  })

  it('bulk-sets priority for checked issues', async () => {
    serve(
      withGroups({ 's-todo': [todo(1), todo(2), todo(3)] }, {}, {
        UpdateIssue: (v: Vars) => ({ updateIssue: { ...todo(Number(String(v?.id).slice(1))), priority: 2 } }),
      }),
    )
    renderAt('/p/YLK')
    await screen.findByText('Issue 3')
    fireEvent.keyDown(document.body, { key: 'j', code: 'KeyJ' })
    fireEvent.keyDown(document.body, { key: 'x', code: 'KeyX' })
    fireEvent.keyDown(document.body, { key: 'j', code: 'KeyJ' })
    fireEvent.keyDown(document.body, { key: 'x', code: 'KeyX' })
    const bar = await screen.findByRole('toolbar', { name: '2 selected' })
    await userEvent.click(within(bar).getByRole('button', { name: 'Priority' }))
    await userEvent.click(screen.getByRole('option', { name: 'High' }))
    await waitFor(() => expect(callsOf('UpdateIssue')).toHaveLength(2))
    expect(callsOf('UpdateIssue').map((v) => v.id).sort()).toEqual(['i1', 'i2'])
    expect(callsOf('UpdateIssue').every((v) => (v.input as { priority: number }).priority === 2)).toBe(true)
  })

  it('shows archived banner and no create button', async () => {
    const archived = project({ archivedAt: '2026-10-30T10:00:00Z' })
    serve({ ...withGroups({ 's-todo': [todo(1)] }), ...signedInAs(owner, [archived]) , IssueGroup: (v: Vars) => conn(stateEq(v) === 's-todo' ? [todo(1)] : []) })
    renderAt('/p/YLK')
    expect(await screen.findByText('This project is archived')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Unarchive' })).toBeInTheDocument()
    // The sidebar's global "New issue" stays; the project page offers no create.
    expect(within(screen.getByRole('main')).queryByRole('button', { name: /Create issue/ })).not.toBeInTheDocument()
    await screen.findByText('Issue 1')
    fireEvent.keyDown(document.body, { key: 'j', code: 'KeyJ' })
    fireEvent.keyDown(document.body, { key: 'x', code: 'KeyX' })
    expect(screen.queryByRole('toolbar')).not.toBeInTheDocument()
  })

  it('groups My issues by state type for the viewer', async () => {
    serve({
      ...signedInAs(owner),
      IssueGroup: (v: Vars) => {
        const f = v?.filter as { assignee?: { isMe?: boolean }; stateType?: { eq?: string } }
        expect(f.assignee).toEqual({ isMe: true })
        return conn(f.stateType?.eq === 'STARTED' ? [issueRow(7, { state: stateOf('s-progress') })] : [])
      },
    })
    renderAt('/my-issues')
    expect(await screen.findByRole('button', { name: 'Started 1' })).toBeInTheDocument()
    expect(await screen.findByText('Issue 7')).toBeInTheDocument()
  })
})
