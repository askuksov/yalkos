import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { conn, issueRow, owner, signedInAs } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, mocked, serve } from '../../test/serve'
import { uploadFile } from '../markdown/upload'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})
vi.mock('../markdown/upload', async (orig) => {
  const actual = await orig<typeof import('../markdown/upload')>()
  return { ...actual, uploadFile: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

let created = 2
function handlers(extra = {}) {
  created = 2
  return {
    ...signedInAs(owner),
    Limits: () => ({ limits: { maxUploadMb: 25 } }),
    IssueGroup: (v: Record<string, unknown> | undefined) =>
      conn((v?.filter as { state?: { eq?: string } })?.state?.eq === 's-todo' ? [issueRow(1)] : []),
    CreateIssue: () => {
      created++
      return { createIssue: { id: `i${created}`, identifier: `YLK-${created}`, project: { id: 'p1' } } }
    },
    ...extra,
  }
}

const openDialogs = () => document.querySelectorAll('dialog[open]').length
const pressC = () => fireEvent.keyDown(document.body, { key: 'c', code: 'KeyC' })

describe('create issue dialog', () => {
  it('opens with C and creates in the current project', async () => {
    serve(handlers())
    renderAt('/p/YLK')
    await screen.findByText('Issue 1')
    pressC()
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    await userEvent.type(within(dialog).getByLabelText('Title'), 'Fix login')
    fireEvent.keyDown(within(dialog).getByLabelText('Title'), { key: 'Enter', ctrlKey: true })
    await waitFor(() => expect(callsOf('CreateIssue')).toHaveLength(1))
    expect(callsOf('CreateIssue')[0].input).toMatchObject({ projectId: 'p1', title: 'Fix login' })
    expect(await screen.findByText('YLK-3 created')).toBeInTheDocument()
    await waitFor(() => expect(openDialogs()).toBe(0))
  })

  it('typing c in the create dialog does not reopen it', async () => {
    serve(handlers())
    renderAt('/p/YLK')
    await screen.findByText('Issue 1')
    pressC()
    const title = await screen.findByLabelText('Title')
    await userEvent.type(title, 'abc')
    expect(title).toHaveValue('abc')
    expect(openDialogs()).toBe(1)
  })

  it('prefills the parent and state from the page', async () => {
    serve(handlers())
    renderAt('/p/YLK')
    await screen.findByText('Issue 1')
    await userEvent.click(screen.getByRole('button', { name: 'Create issue in Todo' }))
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    expect(within(dialog).getByRole('button', { name: 'State' })).toHaveTextContent('Todo')
    await userEvent.type(within(dialog).getByLabelText('Title'), 'In todo')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create issue' }))
    await waitFor(() => expect(callsOf('CreateIssue')[0]?.input).toMatchObject({ stateId: 's-todo' }))
  })

  it('create more keeps properties and clears the title', async () => {
    serve(handlers())
    renderAt('/p/YLK')
    await screen.findByText('Issue 1')
    pressC()
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Priority' }))
    await userEvent.click(screen.getByRole('option', { name: 'High' }))
    await userEvent.click(within(dialog).getByLabelText('Create more'))
    await userEvent.type(within(dialog).getByLabelText('Title'), 'First')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create issue' }))
    await waitFor(() => expect(callsOf('CreateIssue')).toHaveLength(1))
    await waitFor(() => expect(within(dialog).getByLabelText('Title')).toHaveValue(''))
    expect(within(dialog).getByLabelText('Title')).toHaveFocus()
    await userEvent.type(within(dialog).getByLabelText('Title'), 'Second')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create issue' }))
    await waitFor(() => expect(callsOf('CreateIssue')).toHaveLength(2))
    expect(callsOf('CreateIssue')[1].input).toMatchObject({ title: 'Second', priority: 2, projectId: 'p1' })
    expect(openDialogs()).toBe(1)
  })

  it('shows a server field error under the field', async () => {
    serve(
      handlers({
        CreateIssue: () => {
          throw new GraphQLRequestError('INVALID_INPUT', 'title must be at most 255 characters', { field: 'title', reason: 'too_long', params: { max: 255 } })
        },
      }),
    )
    renderAt('/p/YLK')
    await screen.findByText('Issue 1')
    pressC()
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    const title = within(dialog).getByLabelText('Title')
    await userEvent.type(title, 'x')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create issue' }))
    const err = await within(dialog).findByText('Must be at most 255 characters')
    expect(title).toHaveAttribute('aria-describedby', err.id)
    expect(title).toHaveAttribute('aria-invalid', 'true')
  })

  it('opens from the sidebar with the last used project on My issues', async () => {
    localStorage.setItem('yalkos.lastProject', 'p1')
    serve(handlers())
    renderAt('/my-issues')
    await userEvent.click(await screen.findByRole('button', { name: /New issue/ }))
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    expect(await within(dialog).findByRole('button', { name: 'Project' })).toHaveTextContent('YLK')
  })

  it('sends uploads pasted into the description as attachmentIds', async () => {
    vi.mocked(uploadFile).mockResolvedValue({ id: 'a1', url: '/api/attachments/a1/shot.png', name: 'shot.png', size: 3, contentType: 'image/png', inline: true })
    serve(handlers())
    renderAt('/p/YLK')
    await screen.findByText('Issue 1')
    pressC()
    const dialog = await screen.findByRole('dialog', { name: 'New issue' })
    await userEvent.type(within(dialog).getByLabelText('Title'), 'With screenshot')
    const desc = within(dialog).getByRole('textbox', { name: 'Description' })
    fireEvent.paste(desc, { clipboardData: { files: [new File(['abc'], 'shot.png', { type: 'image/png' })], types: ['Files'], getData: () => '' } })
    await waitFor(() => expect(desc).toHaveValue('![shot.png](/api/attachments/a1/shot.png)'))
    expect(vi.mocked(uploadFile)).toHaveBeenCalledWith(expect.any(File), undefined)
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create issue' }))
    await waitFor(() => expect(callsOf('CreateIssue')).toHaveLength(1))
    expect(callsOf('CreateIssue')[0].input).toMatchObject({ description: '![shot.png](/api/attachments/a1/shot.png)', attachmentIds: ['a1'] })
  })
})
