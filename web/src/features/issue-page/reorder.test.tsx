import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { issueRow } from '../../test/fixtures'
import { mocked } from '../../test/serve'
import { ToastProvider } from '../../ui/Toast'
import { childrenKey, useChildReorder } from './useChildReorder'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

describe('sub-issue reorder', () => {
  it('restores the order when the server rejects a reorder', async () => {
    mocked.mockRejectedValue(new GraphQLRequestError('INVALID_INPUT', 'archived', { field: 'sortOrder', reason: 'archived' }))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
    const kids = [issueRow(1, { sortOrder: 100 }), issueRow(2, { sortOrder: 200 }), issueRow(3, { sortOrder: 300 })]
    qc.setQueryData(childrenKey('p'), { issue: { id: 'p', children: { nodes: kids } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>
        <ToastProvider>{children}</ToastProvider>
      </QueryClientProvider>
    )
    const order = () => (qc.getQueryData(childrenKey('p')) as { issue: { children: { nodes: { id: string }[] } } }).issue.children.nodes.map((n) => n.id)
    const { result } = renderHook(() => useChildReorder('p'), { wrapper })
    act(() => result.current.onDragEnd({ active: { id: 'i3' }, over: { id: 'i1' } } as never))
    expect(order()).toEqual(['i3', 'i1', 'i2'])
    await waitFor(() => expect(mocked.mock.calls[0]?.[1]).toEqual({ id: 'i3', input: { sortOrder: -900 } }))
    await waitFor(() => expect(order()).toEqual(['i1', 'i2', 'i3']))
    expect(await screen.findByRole('alert')).toHaveTextContent('The project is archived')
  })
})
