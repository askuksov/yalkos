import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { owner, pageIssue, project, signedInAs, states } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, deferred, mocked, serve } from '../../test/serve'
import { Markdown } from './Markdown'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

type Vars = Record<string, unknown> | undefined

function withIssue(issue: ReturnType<typeof pageIssue>, extra = {}) {
  return {
    ...signedInAs(owner, [project(), project({ id: 'p2', key: 'WEB', name: 'Website' })]),
    IssuePage: () => ({ issue }),
    ...extra,
  }
}

describe('issue page', () => {
  it('redirects a former identifier', async () => {
    serve(withIssue(pageIssue(4, { identifier: 'WEB-4' })))
    renderAt('/issue/YLK-1')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/issue\/WEB-4$/))
  })

  it('shows issue not found', async () => {
    serve({
      ...withIssue(pageIssue(1)),
      IssuePage: () => {
        throw new GraphQLRequestError('NOT_FOUND', 'Issue not found')
      },
    })
    renderAt('/issue/YLK-99')
    expect(await screen.findByText('Issue not found')).toBeInTheDocument()
  })

  it('renders markdown without raw HTML or javascript links', () => {
    const { container } = render(
      <Markdown source={'Hi <img src=x onerror="alert(1)"> <script>alert(2)</script>\n\n[x](javascript:alert(1)) [ok](https://example.com)\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done'} />,
    )
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('script')).toBeNull()
    for (const a of container.querySelectorAll('a')) {
      expect(a.getAttribute('href') ?? '').not.toMatch(/^javascript:/i)
    }
    const ok = screen.getByRole('link', { name: 'ok' })
    expect(ok).toHaveAttribute('target', '_blank')
    expect(ok).toHaveAttribute('rel', 'noopener noreferrer')
    expect(container.querySelector('table')).not.toBeNull()
    expect(container.querySelector('input[type="checkbox"]')).not.toBeNull()
  })

  it('edits the title inline', async () => {
    serve(withIssue(pageIssue(1, { title: 'Old title' }), { UpdateIssue: (v: Vars) => ({ updateIssue: { ...pageIssue(1), title: (v?.input as { title?: string } | undefined)?.title } }) }))
    renderAt('/issue/YLK-1')
    await userEvent.click(await screen.findByRole('button', { name: 'Old title' }))
    const input = screen.getByRole('textbox', { name: 'Title' })
    await userEvent.clear(input)
    await userEvent.type(input, 'New title{Enter}')
    await waitFor(() => expect(callsOf('UpdateIssue')).toEqual([{ id: 'i1', input: { title: 'New title' } }]))
  })

  it('asks before discarding description edits', async () => {
    serve(withIssue(pageIssue(1, { description: 'First' })))
    renderAt('/issue/YLK-1')
    await screen.findByText('First')
    fireEvent.keyDown(document.body, { key: 'e', code: 'KeyE' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    await userEvent.type(area, ' more')
    fireEvent.keyDown(area, { key: 'Escape' })
    const dialog = await screen.findByRole('dialog', { name: 'Discard changes?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Keep editing' }))
    expect(screen.getByRole('textbox', { name: 'Description' })).toHaveValue('First more')
    // Leaving the page asks too.
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Breadcrumb' })).getByRole('link', { name: 'YLK' }))
    const again = await screen.findByRole('dialog', { name: 'Discard changes?' })
    expect(screen.getByTestId('where')).toHaveTextContent('/issue/YLK-1')
    await userEvent.click(within(again).getByRole('button', { name: 'Discard' }))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/YLK$/))
  })

  it('saves the description with Cmd+Enter', async () => {
    serve(withIssue(pageIssue(1, { description: 'First' }), { UpdateIssue: () => ({ updateIssue: pageIssue(1) }) }))
    renderAt('/issue/YLK-1')
    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    const area = screen.getByRole('textbox', { name: 'Description' })
    await userEvent.type(area, '!')
    fireEvent.keyDown(area, { key: 'Enter', metaKey: true })
    await waitFor(() => expect(callsOf('UpdateIssue')).toEqual([{ id: 'i1', input: { description: 'First!' } }]))
  })

  it('changes state optimistically and rolls back on error', async () => {
    const answer = deferred<never>()
    serve(withIssue(pageIssue(1), { UpdateIssue: () => answer.promise }))
    renderAt('/issue/YLK-1')
    const props = await screen.findByRole('complementary', { name: 'Properties' })
    await userEvent.click(within(props).getByRole('button', { name: /Todo/ }))
    await userEvent.click(screen.getByRole('option', { name: 'Done' }))
    expect(within(props).getByRole('button', { name: /Done/ })).toBeInTheDocument()
    expect(callsOf('UpdateIssue')[0]).toEqual({ id: 'i1', input: { stateId: 's-done' } })
    answer.reject(new GraphQLRequestError('INVALID_INPUT', 'archived', { field: 'stateId', reason: 'archived' }))
    expect(await within(props).findByRole('button', { name: /Todo/ })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('The project is archived')
  })

  it('opens the state page of the palette with S', async () => {
    serve(withIssue(pageIssue(1)))
    renderAt('/issue/YLK-1')
    await screen.findByRole('complementary', { name: 'Properties' })
    fireEvent.keyDown(document.body, { key: 's', code: 'KeyS' })
    expect(await screen.findByPlaceholderText('Change state')).toHaveFocus()
  })

  it('saves the due date once, when the field is left', async () => {
    serve(withIssue(pageIssue(1), { UpdateIssue: () => ({ updateIssue: pageIssue(1) }) }))
    renderAt('/issue/YLK-1')
    const props = await screen.findByRole('complementary', { name: 'Properties' })
    const due = within(props).getByLabelText('Due date')
    due.focus()
    // Typing a year fires an input event per digit.
    for (const v of ['0002-11-04', '0020-11-04', '0202-11-04', '2026-11-04']) fireEvent.change(due, { target: { value: v } })
    expect(callsOf('UpdateIssue')).toEqual([])
    fireEvent.blur(due)
    await waitFor(() => expect(callsOf('UpdateIssue')).toEqual([{ id: 'i1', input: { dueDate: '2026-11-04' } }]))
  })

  it('moves the issue after confirmation', async () => {
    serve(
      withIssue(pageIssue(1, { children: { totalCount: 2 } }), {
        MoveIssue: (v: Vars) => {
          expect(v).toEqual({ id: 'i1', projectId: 'p2' })
          return { moveIssue: { id: 'i1', identifier: 'WEB-5' } }
        },
      }),
    )
    renderAt('/issue/YLK-1')
    const props = await screen.findByRole('complementary', { name: 'Properties' })
    await userEvent.click(within(props).getByRole('button', { name: 'Move' }))
    const dialog = await screen.findByRole('dialog', { name: 'Move to project' })
    await userEvent.click(await within(dialog).findByRole('option', { name: /Website/ }))
    expect(within(dialog).getByText('YLK-1 and its sub-issues will get new identifiers in WEB.')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Move' }))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/issue\/WEB-5$/))
  })

  it('deletes after confirmation and goes back to the project', async () => {
    serve(withIssue(pageIssue(1), { DeleteIssue: () => ({ deleteIssue: true }), IssueGroup: () => ({ issues: { totalCount: 0, nodes: [], pageInfo: { hasNextPage: false, endCursor: null } } }) }))
    renderAt('/issue/YLK-1')
    await userEvent.click(await screen.findByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete issue' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/YLK$/))
    expect(await screen.findByText('Issue deleted')).toBeInTheDocument()
  })

  it('is read-only in an archived project', async () => {
    const archived = pageIssue(1, { description: 'Text', project: { id: 'p1', key: 'YLK', name: 'Yalkos', archivedAt: '2026-10-30T10:00:00Z', states } })
    serve(withIssue(archived))
    renderAt('/issue/YLK-1')
    expect(await screen.findByText('This project is archived')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    const props = screen.getByRole('complementary', { name: 'Properties' })
    expect(within(props).queryByRole('button', { name: /Todo/ })).not.toBeInTheDocument()
    expect(within(props).getByLabelText('Due date')).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Issue 1' })).not.toBeInTheDocument()
  })

  it('shows sub-issue progress and adds a sub-issue', async () => {
    const kids = [1, 2, 3].map((k) => ({ ...pageIssue(10 + k), parent: { id: 'i1', identifier: 'YLK-1' } }))
    serve(
      withIssue(pageIssue(1, { subIssueProgress: { completed: 2, total: 3 } }), {
        IssueChildren: () => ({ issue: { id: 'i1', children: { nodes: kids } } }),
      }),
    )
    renderAt('/issue/YLK-1')
    const section = await screen.findByRole('region', { name: 'Sub-issues' })
    expect(within(section).getByText('2/3')).toBeInTheDocument()
    expect(await within(section).findByText('Issue 12')).toBeInTheDocument()
    await userEvent.click(within(section).getByRole('button', { name: 'Add sub-issue' }))
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    expect(within(dialog).getByText('Sub-issue of YLK-1')).toBeInTheDocument()
  })

  it('loads older activity', async () => {
    const entry = (id: string, title: string) => ({
      __typename: 'HistoryEntry', id, action: 'update', actor: { id: 'u1', name: 'Olga' }, createdAt: '2026-10-30T14:02:00Z',
      changes: [{ field: 'title', from: '"x"', to: JSON.stringify(title) }],
    })
    serve(
      withIssue(pageIssue(1), {
        IssueTimeline: (v: Vars) =>
          v?.before === 'c0'
            ? { issue: { id: 'i1', timeline: { nodes: [entry('h1', 'Older')], pageInfo: { hasPreviousPage: false, startCursor: 'c-1' } } } }
            : { issue: { id: 'i1', timeline: { nodes: [entry('h2', 'Newer')], pageInfo: { hasPreviousPage: true, startCursor: 'c0' } } } },
      }),
    )
    renderAt('/issue/YLK-1')
    const activity = await screen.findByRole('region', { name: 'Activity' })
    expect(await within(activity).findByText('Olga renamed the issue to "Newer"')).toBeInTheDocument()
    await userEvent.click(within(activity).getByRole('button', { name: 'Show older' }))
    const older = await within(activity).findByText('Olga renamed the issue to "Older"')
    const items = within(activity).getAllByRole('listitem').map((li) => li.textContent)
    expect(items[0]).toContain('Older')
    expect(items[1]).toContain('Newer')
    expect(older).toBeInTheDocument()
  })
})
