import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { chip, conn, issueRow, labelsData, member, owner, pageIssue, signedInAs, timeline } from '../../test/fixtures'
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

function handlers(viewer = owner, extra = {}) {
  return {
    ...signedInAs(viewer),
    Labels: () => labelsData(),
    Limits: () => ({ limits: { maxUploadMb: 25 } }),
    IssueGroup: inTodo([issueRow(1)]),
    LabelIssueCount: () => ({ issues: { totalCount: 3 } }),
    ...extra,
  }
}

/** One group (Todo) holds the rows; the other state groups are empty. */
const inTodo = (rows: ReturnType<typeof issueRow>[]) => (v: Vars) =>
  conn((v?.filter as { state?: { eq?: string } } | undefined)?.state?.eq === 's-todo' ? rows : [])

const pressKey = (key: string) => fireEvent.keyDown(document.body, { key, code: `Key${key.toUpperCase()}` })

describe('labels and types', () => {
  it('create dialog preselects the default type', async () => {
    serve(handlers(owner, { CreateIssue: () => ({ createIssue: { id: 'i9', identifier: 'YLK-9', project: { id: 'p1' } } }) }))
    renderAt('/p/YLK')
    await screen.findByText('Issue 1')
    pressKey('c')
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    expect(await within(dialog).findByRole('button', { name: 'Type' })).toHaveTextContent('Task')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Type' }))
    await userEvent.click(screen.getByRole('option', { name: 'Bug' }))
    await userEvent.type(within(dialog).getByLabelText('Title'), 'Crash')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create issue' }))
    await waitFor(() => expect(callsOf('CreateIssue')).toHaveLength(1))
    expect(callsOf('CreateIssue')[0].input).toMatchObject({ title: 'Crash', labelIds: ['bug'] })
  })

  it('T opens the type picker on the open issue', async () => {
    serve(handlers(owner, {
      IssuePage: () => ({ issue: pageIssue(1, { type: chip('task'), labels: [chip('task')] }) }),
      IssueTimeline: () => timeline([]),
      IssueAttachments: () => ({ issue: { id: 'i1', attachments: [] } }),
      IssueChildren: () => ({ issue: { id: 'i1', children: { nodes: [] } } }),
      UpdateIssue: () => ({ updateIssue: { ...issueRow(1), type: chip('bug'), labels: [chip('bug')] } }),
    }))
    renderAt('/issue/YLK-1')
    await screen.findByRole('heading', { name: 'Issue 1' })
    pressKey('t')
    const palette = await screen.findByRole('dialog', { name: 'Command palette' })
    await userEvent.click(await within(palette).findByRole('option', { name: 'Bug' }))
    await waitFor(() => expect(callsOf('UpdateIssue')).toEqual([{ id: 'i1', input: { addLabelIds: ['bug'] } }]))
  })

  it('creates a label from the picker', async () => {
    serve(handlers(owner, {
      IssuePage: () => ({ issue: pageIssue(1, { type: chip('task'), labels: [chip('task')] }) }),
      IssueTimeline: () => timeline([]),
      IssueAttachments: () => ({ issue: { id: 'i1', attachments: [] } }),
      IssueChildren: () => ({ issue: { id: 'i1', children: { nodes: [] } } }),
      CreateLabel: (v: Vars) => ({
        createLabel: { id: 'new1', name: (v?.input as { name: string } | undefined)?.name ?? '', color: '#6b6b73', description: '', isGroup: false, exclusive: false, position: 9000, parent: null },
      }),
      UpdateIssue: () => ({ updateIssue: issueRow(1) }),
    }))
    renderAt('/issue/YLK-1')
    const props = await screen.findByRole('complementary', { name: 'Properties' })
    await userEvent.click(await within(props).findByRole('button', { name: 'No labels' }))
    await userEvent.type(screen.getByRole('combobox'), 'Perf2')
    await userEvent.click(screen.getByRole('option', { name: 'Create label "Perf2"' }))
    await waitFor(() => expect(callsOf('UpdateIssue')).toEqual([{ id: 'i1', input: { addLabelIds: ['new1'] } }]))
    expect(callsOf('CreateLabel')[0].input).toMatchObject({ name: 'Perf2' })
  })

  it('shows type first and at most three labels', async () => {
    serve(handlers(owner, {
      IssueGroup: inTodo([issueRow(1, { type: chip('bug'), labels: [chip('backend'), chip('bug'), chip('perf'), chip('security'), chip('frontend')] })]),
    }))
    renderAt('/p/YLK')
    const row = (await screen.findByText('Issue 1')).closest('[role="option"]') as HTMLElement
    const chips = within(row).getAllByTestId('label-chip').map((c) => c.textContent)
    expect(chips).toEqual(['Bug', 'Backend', 'Performance', 'Security'])
    expect(within(row).getByText('+1')).toBeInTheDocument()
  })

  it('filters by type from the URL', async () => {
    serve(handlers())
    renderAt('/p/YLK?kind=bug&label=perf')
    await screen.findByText('Issue 1')
    const f = callsOf('IssueGroup')[0].filter as Record<string, unknown>
    expect(f.type).toEqual({ in: ['bug'] })
    expect(f.label).toEqual({ in: ['perf'] })
    expect(await screen.findByText('Type: Bug')).toBeInTheDocument()
  })

  it('filters by labels an issue must not have', async () => {
    serve(handlers())
    renderAt('/p/YLK?label=perf&nlabel=security,backend')
    await screen.findByText('Issue 1')
    const f = callsOf('IssueGroup')[0].filter as Record<string, unknown>
    expect(f.label).toEqual({ in: ['perf'], nin: ['security', 'backend'] })
    expect(await screen.findByText('Without labels: Security or Backend')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Remove filter: Without labels' }))
    await waitFor(() => expect(screen.queryByText('Without labels: Security or Backend')).toBeNull())
    await waitFor(() => expect(callsOf('IssueGroup').at(-1)?.filter).toMatchObject({ label: { in: ['perf'] } }))
    const last = callsOf('IssueGroup').at(-1)?.filter as { label?: Record<string, unknown> } | undefined
    expect(last?.label?.nin).toBeUndefined()
  })

  it('explains a regroup conflict', async () => {
    serve(handlers(owner, {
      UpdateLabel: () => {
        throw new GraphQLRequestError('INVALID_INPUT', '2 issues would get two labels of this group', { field: 'parentId', reason: 'not_allowed', params: { conflicts: 2 } })
      },
    }))
    renderAt('/settings/labels')
    const row = (await screen.findByDisplayValue('Performance')).closest('li') as HTMLElement
    await userEvent.selectOptions(within(row).getByRole('combobox', { name: 'Group of Performance' }), 'area')
    expect(await screen.findByText('2 issues would get two labels of this group')).toBeInTheDocument()
    expect(callsOf('UpdateLabel')[0]).toEqual({ id: 'perf', input: { parentId: 'area' } })
  })

  it('members see labels settings read-only', async () => {
    serve(handlers(member))
    renderAt('/settings/labels')
    expect(await screen.findByText('Performance')).toBeInTheDocument()
    expect(screen.getByText('Bug')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Delete/ })).toBeNull()
    expect(screen.queryByRole('textbox', { name: /Name of/ })).toBeNull()
    expect(screen.queryByRole('combobox', { name: /Group of/ })).toBeNull()
    expect(screen.queryByRole('radio', { name: /Default type/ })).toBeNull()
  })
})
