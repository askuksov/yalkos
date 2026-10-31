import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, renderHook, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { conn, issueRow, owner, signedInAs, stateOf } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, deferred, mocked, serve } from '../../test/serve'
import { ToastProvider } from '../../ui/Toast'
import { boardKey, useBoardDrag } from './useBoardDrag'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

type Vars = Record<string, unknown> | undefined
const stateEq = (v: Vars) => ((v?.filter as { state?: { eq?: string } })?.state?.eq ?? '') as string
const card = (n: number, stateId: string, sortOrder: number) => issueRow(n, { state: stateOf(stateId), sortOrder })

function seeded() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const page = (nodes: ReturnType<typeof card>[]) => ({ pages: [conn(nodes)], pageParams: [null] })
  qc.setQueryData(boardKey('YLK', 's-todo', ''), page([card(1, 's-todo', 100), card(2, 's-todo', 200), card(3, 's-todo', 300)]))
  qc.setQueryData(boardKey('YLK', 's-done', ''), page([card(4, 's-done', 10), card(5, 's-done', 20)]))
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <ToastProvider>{children}</ToastProvider>
    </QueryClientProvider>
  )
  const ids = (stateId: string) =>
    (qc.getQueryData(boardKey('YLK', stateId, '')) as { pages: { issues: { nodes: { id: string }[] } }[] }).pages.flatMap((p) => p.issues.nodes.map((n) => n.id))
  return { qc, wrapper, ids }
}

const drag = (active: string, over: string | null) => ({ active: { id: active }, over: over ? { id: over } : null }) as never

describe('board', () => {
  it('renders a column per state with cards', async () => {
    serve({ ...signedInAs(owner), IssueGroup: (v: Vars) => conn(stateEq(v) === 's-todo' ? [card(1, 's-todo', 1), card(2, 's-todo', 2)] : []) })
    renderAt('/p/YLK/board')
    const todo = await screen.findByRole('listbox', { name: 'Todo' })
    expect(await within(todo).findByText('Issue 1')).toBeInTheDocument()
    expect(within(todo).getByText('Issue 2')).toBeInTheDocument()
    expect(screen.getAllByRole('listbox').map((c) => c.getAttribute('aria-label'))).toEqual(['Backlog', 'Todo', 'In Progress', 'Done', 'Canceled'])
    // The board always uses the manual order.
    expect(callsOf('IssueGroup').every((v) => (v.orderBy as { field: string }).field === 'SORT_ORDER')).toBe(true)
  })

  it('drop into another column sets state and sort order', async () => {
    const answer = deferred<unknown>()
    mocked.mockImplementation(() => answer.promise as never)
    const { wrapper, ids } = seeded()
    const { result } = renderHook(() => useBoardDrag('YLK', ''), { wrapper })
    act(() => result.current.onDragEnd(drag('i2', 'i5')))
    // Visible in Done before the server answers: placed before Done's second card.
    expect(ids('s-done')).toEqual(['i4', 'i2', 'i5'])
    expect(ids('s-todo')).toEqual(['i1', 'i3'])
    await waitFor(() => expect(mocked.mock.calls[0]?.[1]).toEqual({ id: 'i2', input: { stateId: 's-done', sortOrder: 15 } }))
    answer.resolve({ updateIssue: card(2, 's-done', 15) })
  })

  it('drop on an empty column puts the card at sort order 0', async () => {
    mocked.mockResolvedValue({ updateIssue: card(1, 's-progress', 0) } as never)
    const { wrapper, qc } = seeded()
    qc.setQueryData(boardKey('YLK', 's-progress', ''), { pages: [conn([])], pageParams: [null] })
    const { result } = renderHook(() => useBoardDrag('YLK', ''), { wrapper })
    act(() => result.current.onDragEnd(drag('i1', 'column:s-progress')))
    await waitFor(() => expect(mocked.mock.calls[0]?.[1]).toEqual({ id: 'i1', input: { stateId: 's-progress', sortOrder: 0 } }))
  })

  it('drop within a column only sets sort order', async () => {
    mocked.mockResolvedValue({ updateIssue: card(1, 's-todo', 250) } as never)
    const { wrapper, ids } = seeded()
    const { result } = renderHook(() => useBoardDrag('YLK', ''), { wrapper })
    act(() => result.current.onDragEnd(drag('i1', 'i2')))
    expect(ids('s-todo')).toEqual(['i2', 'i1', 'i3'])
    await waitFor(() => expect(mocked.mock.calls[0]?.[1]).toEqual({ id: 'i1', input: { sortOrder: 250 } }))
  })

  it('rolls back a failed drop', async () => {
    mocked.mockRejectedValue(new GraphQLRequestError('INVALID_INPUT', 'archived', { field: 'stateId', reason: 'archived' }))
    const { wrapper, ids } = seeded()
    const { result } = renderHook(() => useBoardDrag('YLK', ''), { wrapper })
    act(() => result.current.onDragEnd(drag('i2', 'i5')))
    await waitFor(() => expect(ids('s-todo')).toEqual(['i1', 'i2', 'i3']))
    expect(ids('s-done')).toEqual(['i4', 'i5'])
    expect(await screen.findByRole('alert')).toHaveTextContent('The project is archived')
  })

  it('navigates columns with H and L', async () => {
    serve({
      ...signedInAs(owner),
      IssueGroup: (v: Vars) =>
        conn(stateEq(v) === 's-todo' ? [card(1, 's-todo', 1), card(2, 's-todo', 2)] : stateEq(v) === 's-progress' ? [card(7, 's-progress', 1)] : []),
    })
    renderAt('/p/YLK/board')
    await screen.findByText('Issue 7')
    const selected = () => document.querySelector('[data-row-id][aria-selected="true"]')?.getAttribute('data-row-id')
    const key = (k: string) => fireEvent.keyDown(document.body, { key: k, code: `Key${k.toUpperCase()}` })
    key('j')
    expect(selected()).toBe('i1')
    key('j')
    expect(selected()).toBe('i2')
    key('l')
    expect(selected()).toBe('i7')
    key('h')
    expect(selected()).toBe('i1')
    fireEvent.keyDown(document.body, { key: 'Enter', code: 'Enter' })
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/issue/YLK-1'))
  })
})
