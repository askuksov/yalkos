import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, renderHook, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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

  describe('dragging a card', () => {
    const lay: { spy?: ReturnType<typeof vi.spyOn> } = {}
    afterEach(() => lay.spy?.mockRestore())

    // Lay the columns out left to right; each card sits 20 px further right than the one above it.
    // `emptyHeight` is the height of a column's card list (the drop area of an empty column).
    function layOutColumns(spacing = 300, emptyHeight = 400) {
      lay.spy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
        const column = this.closest<HTMLElement>('[data-column]')
        // The drag overlay (or its wrapper) is what the collision uses as the dragged rect: place it over the source card.
        if (!column) return this.querySelector('[data-drag-overlay]') || this.hasAttribute('data-drag-overlay') ? document.querySelector<HTMLElement>('[data-row-id="i2"]')!.getBoundingClientRect() : new DOMRect(0, 0, 0, 0)
        const x = [...column.parentElement!.children].indexOf(column) * spacing
        if (this === column) return new DOMRect(x, 0, 280, 440)
        const row = this.dataset.rowId ? [...this.parentElement!.children].indexOf(this) : -1
        return row < 0 ? new DOMRect(x, 36, 280, emptyHeight) : new DOMRect(x + 8 + row * 20, 36 + row * 50, 264, 40)
      })
    }

    // jsdom has no layout, so the keyboard sensor drives the drag: focus the card, Space, then an arrow.
    async function startDrag(arrow = 'ArrowRight') {
      serve({
        ...signedInAs(owner),
        IssueGroup: (v: Vars) => conn(stateEq(v) === 's-todo' ? [card(1, 's-todo', 1), card(2, 's-todo', 2)] : stateEq(v) === 's-done' ? [card(4, 's-done', 1)] : []),
      })
      renderAt('/p/YLK/board')
      await screen.findByText('Issue 2')
      const source = document.querySelector<HTMLElement>('[data-row-id="i2"]')!
      source.focus()
      fireEvent.keyDown(source, { code: 'Space', key: ' ' })
      // The source card dims once the sensor has started the drag; its key listener is attached right after.
      await waitFor(() => expect(source.style.opacity).toBe('0.5'))
      fireEvent.keyDown(document, { code: arrow, key: arrow })
      return source
    }

    it('shows a copy of the card in the drag overlay without a row id', async () => {
      await startDrag()
      const overlay = await waitFor(() => {
        const found = document.querySelector<HTMLElement>('[data-drag-overlay]')
        expect(found).not.toBeNull()
        return found!
      })
      expect(within(overlay).getByText('YLK-2')).toBeInTheDocument()
      expect(within(overlay).getByText('Issue 2')).toBeInTheDocument()
      expect(overlay.hasAttribute('data-row-id')).toBe(false)
      expect(overlay.querySelector('[data-row-id]')).toBeNull()
      // A hidden copy with no link, so it adds no second link or tab stop.
      expect(overlay).toHaveAttribute('aria-hidden', 'true')
      expect(overlay.querySelector('a')).toBeNull()
      // Keyboard navigation still sees one card per issue.
      expect(document.querySelectorAll('[data-row-id="i2"]')).toHaveLength(1)
      fireEvent.keyDown(document, { code: 'Escape', key: 'Escape' })
      await waitFor(() => expect(document.querySelector('[data-drag-overlay]')).toBeNull())
    })

    it('does not move the source card sideways while it is dragged', async () => {
      layOutColumns()
      // Up to the card above: a drop spot inside the same column, 20 px to the left.
      const source = await startDrag('ArrowUp')
      // Without the overlay the source itself follows the drag (here by -20px), which makes the column scroll sideways.
      expect(source.style.transform).not.toMatch(/translate3d\((?!0px)-?[\d.]+px/)
      fireEvent.keyDown(document, { code: 'Escape', key: 'Escape' })
    })

    // Columns whose outline class is set (the CSS module class is "over", possibly hashed).
    const outlined = () => [...document.querySelectorAll<HTMLElement>('[data-column]')].filter((c) => /over/.test(c.className)).map((c) => c.dataset.column)

    it('outlines another column while the card is over one of its cards', async () => {
      // Columns 30 px apart: the card of Done is the nearest drop spot to the right of the dragged one.
      layOutColumns(30)
      await startDrag()
      await waitFor(() => expect(outlined()).toEqual(['s-done']))
      fireEvent.keyDown(document, { code: 'Escape', key: 'Escape' })
      await waitFor(() => expect(outlined()).toEqual([]))
    })

    it('outlines an empty column while the card is over it', async () => {
      // Only the list of In Progress (empty) is near; its height is small so it is the nearest drop spot.
      layOutColumns(30, 60)
      await startDrag()
      await waitFor(() => expect(outlined()).toEqual(['s-progress']))
      fireEvent.keyDown(document, { code: 'Escape', key: 'Escape' })
    })

    it('does not outline the source column while the card is reordered inside it', async () => {
      layOutColumns()
      await startDrag('ArrowUp')
      await waitFor(() => expect(document.querySelector('[data-drag-overlay]')).not.toBeNull())
      expect(outlined()).toEqual([])
      fireEvent.keyDown(document, { code: 'Escape', key: 'Escape' })
    })
  })
})
