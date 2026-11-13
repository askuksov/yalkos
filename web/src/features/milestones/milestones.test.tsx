import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, renderHook, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { conn, issueRow, labelsData, milestone, owner, pageIssue, project, signedInAs, timeline } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, mocked, serve } from '../../test/serve'
import { ToastProvider } from '../../ui/Toast'
import { useMilestoneReorder } from './useMilestoneReorder'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

type Vars = Record<string, unknown> | undefined

const bank = milestone(1, {
  name: 'Bank APIs', status: 'IN_PROGRESS', progress: { completed: 12, total: 30 }, targetDate: '2026-01-01', overdue: true,
  lead: { id: owner.id, name: owner.name },
})
const launch = milestone(2, { name: 'Launch', targetDate: '2027-05-01' })

function handlers(extra = {}) {
  return {
    ...signedInAs(owner, [project(), project({ id: 'p2', key: 'WEB', name: 'Website' })]),
    Labels: () => labelsData(),
    Limits: () => ({ limits: { maxUploadMb: 25 } }),
    Milestones: () => ({ project: { id: 'p1', milestones: [bank, launch] } }),
    MilestonePage: () => ({ milestone: { ...bank, description: 'Connect *banks*', createdBy: { id: owner.id } } }),
    IssueGroup: (v: Vars) => conn((v?.filter as { state?: { eq?: string } } | undefined)?.state?.eq === 's-todo' ? [issueRow(1)] : []),
    IssueTimeline: () => timeline([]),
      IssueAttachments: () => ({ issue: { id: 'i1', attachments: [] } }),
    IssueChildren: () => ({ issue: { id: 'i1', children: { nodes: [] } } }),
    ...extra,
  }
}

const pressKey = (key: string) => fireEvent.keyDown(document.body, { key, code: `Key${key.toUpperCase()}` })

describe('milestones', () => {
  it('lists milestones with progress and overdue date', async () => {
    serve(handlers())
    renderAt('/p/YLK/milestones')
    const row = (await screen.findByRole('link', { name: 'Bank APIs' })).closest('li') as HTMLElement
    expect(within(row).getByText('12/30')).toBeInTheDocument()
    expect(within(row).getByText('In progress')).toBeInTheDocument()
    expect(within(row).getByText('2026-01-01')).toHaveAttribute('data-overdue', 'true')
    expect(within(row).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '12')
    const other = screen.getByRole('link', { name: 'Launch' }).closest('li') as HTMLElement
    expect(within(other).getByText('2027-05-01')).not.toHaveAttribute('data-overdue', 'true')
    expect(within(other).getByText('Planned')).toBeInTheDocument()
  })

  it('opens the new milestone dialog when ?new=1 arrives on the mounted page', async () => {
    serve(handlers())
    const { router } = renderAt('/p/YLK/milestones')
    await screen.findByRole('link', { name: 'Bank APIs' })
    expect(screen.queryByRole('dialog', { name: 'New milestone' })).not.toBeInTheDocument()
    await act(() => router.navigate('/p/YLK/milestones?new=1'))
    expect(await screen.findByRole('dialog', { name: 'New milestone' })).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('opens the dialog on initial ?new=1 (control)', async () => {
    serve(handlers())
    renderAt('/p/YLK/milestones?new=1')
    expect(await screen.findByRole('dialog', { name: 'New milestone' })).toBeInTheDocument()
  })

  it('drops ?new=1 after an initial open', async () => {
    serve(handlers())
    const { router } = renderAt('/p/YLK/milestones?new=1')
    await screen.findByRole('dialog', { name: 'New milestone' })
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('does not reopen the dialog after it was closed', async () => {
    serve(handlers())
    const { router } = renderAt('/p/YLK/milestones')
    await screen.findByRole('link', { name: 'Bank APIs' })
    await act(() => router.navigate('/p/YLK/milestones?new=1'))
    const dialog = await screen.findByRole('dialog', { name: 'New milestone' })
    await waitFor(() => expect(router.state.location.search).toBe(''))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New milestone' })).not.toBeInTheDocument())
    expect(router.state.location.search).toBe('')
  })

  it('keeps other search params when dropping new', async () => {
    serve(handlers())
    const { router } = renderAt('/p/YLK/milestones')
    await screen.findByRole('link', { name: 'Bank APIs' })
    await act(() => router.navigate('/p/YLK/milestones?new=1&x=2'))
    await screen.findByRole('dialog', { name: 'New milestone' })
    await waitFor(() => expect(router.state.location.search).toBe('?x=2'))
  })

  it('does not open the dialog on an archived project', async () => {
    serve({
      ...handlers(),
      ...signedInAs(owner, [project({ archivedAt: '2026-01-01T00:00:00Z' })]),
    })
    const { router } = renderAt('/p/YLK/milestones')
    await screen.findByRole('link', { name: 'Bank APIs' })
    await act(() => router.navigate('/p/YLK/milestones?new=1'))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(screen.queryByRole('dialog', { name: 'New milestone' })).not.toBeInTheDocument()
  })

  it('sets a manual status and returns to automatic', async () => {
    serve(handlers({ UpdateMilestone: () => ({ updateMilestone: { ...bank, description: '' } }) }))
    renderAt('/p/YLK/milestones/m1')
    const status = await screen.findByRole('combobox', { name: 'Status' })
    expect(within(status).getByRole('option', { name: 'Automatic (In progress)' })).toBeInTheDocument()
    await userEvent.selectOptions(status, 'CANCELED')
    await waitFor(() => expect(callsOf('UpdateMilestone')).toEqual([{ id: 'm1', input: { status: 'CANCELED' } }]))
    await userEvent.selectOptions(status, 'auto')
    await waitFor(() => expect(callsOf('UpdateMilestone')[1]).toEqual({ id: 'm1', input: { statusAutomatic: true } }))
  })

  it('keeps the description draft when another field saves', async () => {
    serve(handlers({ UpdateMilestone: () => ({ updateMilestone: { ...bank, description: 'Connect *banks*' } }) }))
    renderAt('/p/YLK/milestones/m1')
    await userEvent.click(await screen.findByRole('button', { name: 'Edit description' }))
    const box = screen.getByRole('textbox', { name: 'Description' })
    await userEvent.type(box, ' and cards')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Status' }), 'CANCELED')
    await waitFor(() => expect(callsOf('UpdateMilestone')).toHaveLength(1))
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.getByRole('textbox', { name: 'Description' })).toHaveValue('Connect *banks* and cards')
  })

  it('shows milestone issues as a list with a fixed filter', async () => {
    serve(handlers())
    renderAt('/p/YLK/milestones/m1')
    expect(await screen.findByText('Issue 1')).toBeInTheDocument()
    const filters = callsOf('IssueGroup').map((c) => c.filter as Record<string, unknown>)
    expect(filters.length).toBeGreaterThan(0)
    for (const f of filters) expect(f.milestone).toEqual({ eq: 'm1' })
    expect(screen.getByText('Milestone: Bank APIs')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove filter: Milestone' })).toBeNull()
  })

  it('M sets the milestone of the open issue', async () => {
    serve(handlers({
      IssuePage: () => ({ issue: pageIssue(1) }),
      UpdateIssue: () => ({ updateIssue: { ...issueRow(1), milestone: { id: 'm1', name: 'Bank APIs' } } }),
    }))
    renderAt('/issue/YLK-1')
    await screen.findByRole('complementary', { name: 'Properties' })
    pressKey('m')
    const palette = await screen.findByRole('dialog', { name: 'Command palette' })
    await userEvent.click(await within(palette).findByRole('option', { name: 'Bank APIs' }))
    await waitFor(() => expect(callsOf('UpdateIssue')).toEqual([{ id: 'i1', input: { milestoneId: 'm1' } }]))
  })

  it('move confirm mentions the milestone', async () => {
    serve(handlers({ IssuePage: () => ({ issue: pageIssue(1, { milestone: { id: 'm1', name: 'Bank APIs' } }) }) }))
    renderAt('/issue/YLK-1')
    const props = await screen.findByRole('complementary', { name: 'Properties' })
    await userEvent.click(within(props).getByRole('button', { name: 'Move' }))
    const dialog = await screen.findByRole('dialog', { name: 'Move to project' })
    await userEvent.click(await within(dialog).findByRole('option', { name: /WEB/ }))
    expect(await within(dialog).findByText(/Its milestone will be removed\./)).toBeInTheDocument()
  })

  it('reorders milestones by drag end', async () => {
    mocked.mockResolvedValue({ reorderMilestones: [] } as never)
    const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>
        <ToastProvider>{children}</ToastProvider>
      </QueryClientProvider>
    )
    const { result } = renderHook(() => useMilestoneReorder('p1', 'YLK', ['a', 'b', 'c']), { wrapper })
    act(() => result.current.onDragEnd({ active: { id: 'c' }, over: { id: 'a' } } as never))
    await waitFor(() => expect(mocked.mock.calls[0]?.[1]).toEqual({ projectId: 'p1', milestoneIds: ['c', 'a', 'b'] }))
  })
})
