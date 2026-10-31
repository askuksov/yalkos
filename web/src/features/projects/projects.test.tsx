import { act, renderHook, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { conn, member, owner, project, signedInAs } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, mocked, serve } from '../../test/serve'
import { ToastProvider } from '../../ui/Toast'
import { useStateReorder } from './useStateReorder'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

type Vars = Record<string, unknown> | undefined

const browse = (nodes: ReturnType<typeof project>[]) => () => ({
  projects: { nodes: nodes.map((p) => ({ id: p.id, key: p.key, name: p.name, description: '', color: null, archivedAt: p.archivedAt, isMember: p.isMember, members: p.members })) },
})

describe('projects', () => {
  it('creates a project with the suggested key', async () => {
    serve({
      ...signedInAs(owner),
      BrowseProjects: browse([project()]),
      IssueGroup: () => conn([]),
      CreateProject: (v: Vars) => ({ createProject: { id: 'p9', key: (v?.input as { key?: string } | undefined)?.key } }),
    })
    renderAt('/projects')
    await userEvent.click(await screen.findByRole('button', { name: 'Create project' }))
    const dialog = await screen.findByRole('dialog', { name: 'Create project' })
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Yalkos Web App')
    expect(within(dialog).getByLabelText('Key')).toHaveValue('YWA')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create project' }))
    await waitFor(() => expect(callsOf('CreateProject')).toEqual([{ input: { name: 'Yalkos Web App', key: 'YWA' } }]))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/YWA$/))
  })

  it('joins and leaves a project', async () => {
    let joined = false
    serve({
      ...signedInAs(member),
      BrowseProjects: () => browse([project({ isMember: joined })])(),
      AddProjectMember: () => {
        joined = true
        return { addProjectMember: { id: 'p1', isMember: true } }
      },
      RemoveProjectMember: () => {
        joined = false
        return { removeProjectMember: { id: 'p1', isMember: false } }
      },
    })
    renderAt('/projects')
    await userEvent.click(await screen.findByRole('button', { name: 'Join' }))
    await waitFor(() => expect(callsOf('AddProjectMember')).toEqual([{ projectId: 'p1', userId: member.id }]))
    await userEvent.click(await screen.findByRole('button', { name: 'Leave' }))
    await waitFor(() => expect(callsOf('RemoveProjectMember')).toEqual([{ projectId: 'p1', userId: member.id }]))
    expect(screen.queryByRole('button', { name: 'Create project' })).not.toBeInTheDocument()
  })

  it('reorders states by drag end', async () => {
    mocked.mockResolvedValue({ reorderStates: [] } as never)
    const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>
        <ToastProvider>{children}</ToastProvider>
      </QueryClientProvider>
    )
    const { result } = renderHook(() => useStateReorder('p1', ['a', 'b', 'c', 'd']), { wrapper })
    act(() => result.current.onDragEnd({ active: { id: 'd' }, over: { id: 'b' } } as never))
    await waitFor(() => expect(mocked.mock.calls[0]?.[1]).toEqual({ projectId: 'p1', stateIds: ['a', 'd', 'b', 'c'] }))
  })

  it('requires a move target when deleting a used state', async () => {
    serve({
      ...signedInAs(owner),
      StateIssueCount: () => ({ issues: { totalCount: 3 } }),
      DeleteState: () => ({ deleteState: true }),
    })
    renderAt('/p/YLK/settings')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete In Progress' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete state' })
    expect(await within(dialog).findByText('3 issues are in In Progress. Move them to:')).toBeInTheDocument()
    const del = within(dialog).getByRole('button', { name: 'Delete' })
    expect(del).toBeDisabled()
    await userEvent.selectOptions(within(dialog).getByLabelText('Move issues to'), 's-todo')
    expect(del).toBeEnabled()
    await userEvent.click(del)
    await waitFor(() => expect(callsOf('DeleteState')).toEqual([{ id: 's-progress', moveIssuesTo: 's-todo' }]))
  })

  it('owner deletes only after typing the key', async () => {
    serve({ ...signedInAs(owner), BrowseProjects: browse([]), DeleteProject: () => ({ deleteProject: true }) })
    renderAt('/p/YLK/settings')
    const input = await screen.findByLabelText('Type YLK to confirm')
    const del = screen.getByRole('button', { name: 'Delete project' })
    expect(del).toBeDisabled()
    await userEvent.type(input, 'YL')
    expect(del).toBeDisabled()
    await userEvent.type(input, 'K')
    await userEvent.click(del)
    await waitFor(() => expect(callsOf('DeleteProject')).toEqual([{ id: 'p1', confirmKey: 'YLK' }]))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/projects$/))
  })

  it('navigates to the new key after a key change', async () => {
    serve({
      ...signedInAs(owner, [project(), project({ key: 'YK' })]),
      UpdateProject: (v: Vars) => ({ updateProject: { id: 'p1', key: (v?.input as { key?: string } | undefined)?.key, name: 'Yalkos', description: '', icon: null, color: null } }),
    })
    renderAt('/p/YLK/settings')
    const key = await screen.findByLabelText('Key')
    await userEvent.clear(key)
    await userEvent.type(key, 'yk')
    expect(key).toHaveValue('YK')
    expect(screen.getByText('Old identifiers keep working')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(callsOf('UpdateProject')).toEqual([{ id: 'p1', input: { key: 'YK' } }]))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/YK\/settings$/))
  })

  it('members see settings read-only', async () => {
    serve(signedInAs(member))
    renderAt('/p/YLK/settings')
    expect(await screen.findByText('Only owners and admins can change project settings.')).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Delete / })).not.toBeInTheDocument()
    expect(screen.queryByText('Danger zone')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Name of Todo')).toHaveAttribute('readonly')
  })
})
