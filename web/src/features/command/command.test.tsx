import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { conn, owner, pageIssue, project, signedInAs } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, mocked, serve } from '../../test/serve'
import { pushRecent } from './recent'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

type Vars = Record<string, unknown> | undefined
const searchHit = { id: 'i7', identifier: 'YLK-7', title: 'Fix login redirect', project: { id: 'p1', key: 'YLK' }, state: { name: 'Todo', type: 'UNSTARTED', color: '#888' } }

function handlers(extra = {}) {
  return {
    ...signedInAs(owner),
    IssueGroup: () => conn([]),
    PaletteSearch: (v: Vars) => ({ issues: { nodes: (v?.filter as { search?: string })?.search === 'login' ? [searchHit] : [] } }),
    PaletteIssue: (v: Vars) => {
      if (v?.id !== 'YLK-2') throw new GraphQLRequestError('NOT_FOUND', 'Issue not found')
      return { issue: { id: 'i2', identifier: 'YLK-2', title: 'Two', project: { id: 'p1', key: 'YLK' } } }
    },
    ...extra,
  }
}

const modK = () => fireEvent.keyDown(document.body, { key: 'k', code: 'KeyK', ctrlKey: true })

describe('command palette', () => {
  it('opens with mod+k and searches issues', async () => {
    serve(handlers())
    renderAt('/my-issues')
    await screen.findByRole('navigation', { name: 'Main' })
    modK()
    const dialog = await screen.findByRole('dialog', { name: 'Command palette' })
    await userEvent.type(within(dialog).getByRole('combobox'), 'login')
    expect(await within(dialog).findByText('Fix login redirect')).toBeInTheDocument()
    expect(callsOf('PaletteSearch').some((v) => (v.filter as { search: string }).search === 'login' && v.first === 10)).toBe(true)
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/issue/YLK-7'))
  })

  it('opens an exact identifier first', async () => {
    serve(handlers())
    renderAt('/my-issues')
    await screen.findByRole('navigation', { name: 'Main' })
    modK()
    const dialog = await screen.findByRole('dialog', { name: 'Command palette' })
    await userEvent.type(within(dialog).getByRole('combobox'), 'ylk-2')
    const first = await within(dialog).findByRole('option', { name: 'Open YLK-2' })
    expect(within(dialog).getAllByRole('option')[0]).toBe(first)
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/issue\/YLK-2$/))
  })

  it('changes state of the open issue from a nested page', async () => {
    serve(handlers({ IssuePage: () => ({ issue: pageIssue(1) }), UpdateIssue: () => ({ updateIssue: pageIssue(1) }) }))
    renderAt('/issue/YLK-1')
    await screen.findByRole('complementary', { name: 'Properties' })
    fireEvent.keyDown(document.body, { key: 's', code: 'KeyS' })
    const dialog = await screen.findByRole('dialog', { name: 'Command palette' })
    await userEvent.click(await within(dialog).findByRole('option', { name: 'Done' }))
    await waitFor(() => expect(callsOf('UpdateIssue')).toEqual([{ id: 'i1', input: { stateId: 's-done' } }]))
  })

  it('names the sub-issues when moving from the palette', async () => {
    serve(
      handlers({
        ...signedInAs(owner, [project(), project({ id: 'p2', key: 'WEB', name: 'Website' })]),
        IssuePage: () => ({ issue: pageIssue(1, { children: { totalCount: 2 } }) }),
        PaletteChildCount: () => ({ issue: { id: 'i1', children: { totalCount: 2 } } }),
      }),
    )
    renderAt('/issue/YLK-1')
    await screen.findByRole('complementary', { name: 'Properties' })
    modK()
    const dialog = await screen.findByRole('dialog', { name: 'Command palette' })
    await userEvent.click(within(dialog).getByRole('option', { name: 'Move to project…' }))
    await userEvent.click(await within(dialog).findByRole('option', { name: /Website/ }))
    expect(await within(dialog).findByRole('option', { name: 'YLK-1 and its sub-issues will get new identifiers in WEB.' })).toBeInTheDocument()
  })

  it('goes back with Backspace on empty input', async () => {
    serve(handlers({ IssuePage: () => ({ issue: pageIssue(1) }) }))
    renderAt('/issue/YLK-1')
    await screen.findByRole('complementary', { name: 'Properties' })
    modK()
    const dialog = await screen.findByRole('dialog', { name: 'Command palette' })
    await userEvent.click(within(dialog).getByRole('option', { name: 'Set priority…' }))
    expect(within(dialog).getByPlaceholderText('Set priority')).toBeInTheDocument()
    await userEvent.keyboard('{Backspace}')
    expect(within(dialog).getByPlaceholderText('Type a command or search…')).toBeInTheDocument()
  })

  it('shows recent issues when empty', async () => {
    pushRecent({ id: 'i3', identifier: 'YLK-3', projectId: 'p1', title: 'Board drag' })
    serve(handlers())
    renderAt('/my-issues')
    await screen.findByRole('navigation', { name: 'Main' })
    modK()
    const dialog = await screen.findByRole('dialog', { name: 'Command palette' })
    expect(within(dialog).getByRole('group', { name: 'Recent' })).toHaveTextContent('YLK-3Board drag')
  })

  it('opens shortcuts help with ?', async () => {
    serve(handlers())
    renderAt('/my-issues')
    await screen.findByRole('navigation', { name: 'Main' })
    fireEvent.keyDown(document.body, { key: '?', code: 'Slash', shiftKey: true })
    const help = await screen.findByRole('dialog', { name: 'Keyboard shortcuts' })
    expect(within(help).getByText('Command palette')).toBeInTheDocument()
  })
})
