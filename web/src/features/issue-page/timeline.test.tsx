import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { i18n } from '../../i18n'
import { comment, event, labelsData, member, milestone, owner, pageIssue, signedInAs, timeline } from '../../test/fixtures'
import { renderAt } from '../../test/render'
import { callsOf, mocked, serve } from '../../test/serve'
import { uploadFile } from '../markdown/upload'
import { describe as describeEntry, type DescribeContext } from './history'

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
  vi.mocked(uploadFile).mockReset()
})

type Vars = Record<string, unknown> | undefined

const shot = {
  id: 'a1', name: 'shot.png', size: 2048, contentType: 'image/png', url: '/api/attachments/a1/shot.png', inline: true,
  createdAt: '2026-10-30T12:10:00Z', uploader: { id: owner.id, name: owner.name },
}

function handlers(extra = {}, issue = pageIssue(1)) {
  return {
    ...signedInAs(owner),
    Labels: () => labelsData(),
    Limits: () => ({ limits: { maxUploadMb: 25 } }),
    Milestones: () => ({ project: { id: 'p1', milestones: [milestone(1, { name: 'Bank APIs' })] } }),
    IssuePage: () => ({ issue }),
    IssueChildren: () => ({ issue: { id: 'i1', children: { nodes: [] } } }),
    IssueTimeline: () => timeline([]),
    IssueAttachments: () => ({ issue: { id: 'i1', attachments: [] } }),
    ...extra,
  }
}

async function openIssue() {
  renderAt('/issue/YLK-1')
  return screen.findByRole('region', { name: 'Activity' })
}

describe('issue timeline', () => {
  it('renders comments and changes in time order', async () => {
    serve(handlers({
      IssueTimeline: () =>
        timeline([
          event('h1', [['title', null, 'Issue 1']], { action: 'create', at: '2026-10-30T12:00:00Z' }),
          comment('c1', 'First **comment**', { at: '2026-10-30T12:05:00Z', author: member }),
          event('h2', [['priority', 0, 2]], { at: '2026-10-30T12:07:00Z' }),
        ]),
    }))
    const activity = await openIssue()
    const items = await within(activity).findAllByRole('listitem')
    expect(items.map((li) => li.textContent)).toEqual([
      expect.stringContaining('Olga created the issue'),
      expect.stringContaining('First comment'),
      expect.stringContaining('Olga set priority to High'),
    ])
    expect(within(items[1]).getByText('Andrii')).toBeInTheDocument()
    expect(within(items[1]).getByText('comment').tagName).toBe('STRONG')
  })

  it('posts a comment with Cmd+Enter and clears the draft', async () => {
    serve(handlers({ CreateComment: (v: Vars) => ({ createComment: { id: 'c9', body: v?.body } }) }))
    const activity = await openIssue()
    const box = within(activity).getByRole('textbox', { name: 'Comment' })
    await userEvent.type(box, 'Hello there')
    expect(localStorage.getItem('yalkos.draft.comment.i1')).toBe('Hello there')
    fireEvent.keyDown(box, { key: 'Enter', metaKey: true })
    await waitFor(() => expect(callsOf('CreateComment')).toEqual([{ issueId: 'i1', body: 'Hello there' }]))
    // The editor starts afresh (its upload list too) after posting.
    await waitFor(() => expect(within(activity).getByRole('textbox', { name: 'Comment' })).toHaveValue(''))
    expect(localStorage.getItem('yalkos.draft.comment.i1')).toBeNull()
  })

  it('restores a comment draft after reload', async () => {
    localStorage.setItem('yalkos.draft.comment.i1', 'Half-written thought')
    serve(handlers())
    const activity = await openIssue()
    expect(within(activity).getByRole('textbox', { name: 'Comment' })).toHaveValue('Half-written thought')
  })

  it('edits own comment and shows (edited)', async () => {
    let edited: string | null = null
    let body = 'Typo here'
    serve(handlers({
      IssueTimeline: () => timeline([comment('c1', body, { editedAt: edited }), comment('c2', 'Not mine', { author: member })]),
      UpdateComment: (v: Vars) => {
        body = String(v?.body)
        edited = '2026-10-30T13:00:00Z'
        return { updateComment: { id: 'c1', body, editedAt: edited } }
      },
    }))
    const activity = await openIssue()
    const mine = (await within(activity).findByText('Typo here')).closest('li') as HTMLElement
    const other = within(activity).getByText('Not mine').closest('li') as HTMLElement
    expect(within(other).queryByRole('button', { name: 'Edit comment' })).toBeNull()
    expect(within(other).getByRole('button', { name: 'Delete comment' })).toBeInTheDocument()
    await userEvent.click(within(mine).getByRole('button', { name: 'Edit comment' }))
    const box = within(mine).getByRole('textbox', { name: 'Edit comment' })
    await userEvent.clear(box)
    await userEvent.type(box, 'Fixed')
    await userEvent.click(within(mine).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(callsOf('UpdateComment')).toEqual([{ id: 'c1', body: 'Fixed' }]))
    expect(await within(activity).findByText('(edited)')).toBeInTheDocument()
    expect(within(activity).getByText('Fixed')).toBeInTheDocument()
  })

  it('describes label, type, milestone and attachment changes', () => {
    const ctx: DescribeContext = {
      t: i18n.getFixedT(null, ['issue', 'common']),
      users: new Map(), states: new Map(), issues: new Map(), projects: new Map(),
      labels: new Map([
        ['task', { name: 'Task', type: true }], ['bug', { name: 'Bug', type: true }],
        ['perf', { name: 'Performance', type: false }], ['sec', { name: 'Security', type: false }],
      ]),
      milestones: new Map([['m1', 'Bank APIs']]),
    }
    const lines = (changes: [string, unknown, unknown][]) => describeEntry(event('h', changes), ctx)
    expect(lines([['labelIds', ['task'], ['perf', 'task']]])).toEqual(['Olga added label Performance'])
    expect(lines([['labelIds', ['sec', 'task'], ['task']]])).toEqual(['Olga removed label Security'])
    expect(lines([['labelIds', [], ['bug']]])).toEqual(['Olga set type to Bug'])
    expect(lines([['labelIds', ['task', 'perf'], ['bug', 'perf']]])).toEqual(['Olga changed type from Task to Bug'])
    expect(lines([['labelIds', ['bug'], []]])).toEqual(['Olga removed type Bug'])
    expect(lines([['labelIds', ['gone'], []]])).toEqual(['Olga removed label a deleted label'])
    expect(lines([['milestoneId', null, 'm1']])).toEqual(['Olga added the issue to milestone Bank APIs'])
    expect(lines([['milestoneId', 'm1', null]])).toEqual(['Olga removed it from milestone Bank APIs'])
    expect(lines([['attachmentAdded', null, { id: 'a1', name: 'shot.png', size: 3 }]])).toEqual(['Olga attached shot.png'])
    expect(lines([['attachmentDeleted', { id: 'a1', name: 'shot.png' }, null]])).toEqual(['Olga deleted shot.png'])
    expect(lines([['commentDeleted', 'c1', null]])).toEqual(['Olga deleted a comment'])
  })

  it('warns before deleting a file used in the description', async () => {
    serve(handlers(
      { IssueAttachments: () => ({ issue: { id: 'i1', attachments: [shot] } }), DeleteAttachment: () => ({ deleteAttachment: true }) },
      pageIssue(1, { description: 'Look: ![shot](/api/attachments/a1/shot.png)' }),
    ))
    renderAt('/issue/YLK-1')
    const files = await screen.findByRole('region', { name: 'Attachments' })
    const row = (await within(files).findByRole('link', { name: 'shot.png' })).closest('li') as HTMLElement
    expect(within(row).getByText('2 KB')).toBeInTheDocument()
    await userEvent.click(within(row).getByRole('button', { name: 'Delete shot.png' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete shot.png?' })
    expect(within(dialog).getByText('This file is used in the description or comments.')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(callsOf('DeleteAttachment')).toEqual([{ id: 'a1' }]))
  })

  it('drops a file on the page to attach it', async () => {
    vi.mocked(uploadFile).mockResolvedValue({ id: 'a2', url: '/api/attachments/a2/notes.txt', name: 'notes.txt', size: 5, contentType: 'text/plain', inline: false })
    serve(handlers())
    renderAt('/issue/YLK-1')
    const files = await screen.findByRole('region', { name: 'Attachments' })
    const page = files.closest('[data-drop-zone]') as HTMLElement
    const file = new File(['hello'], 'notes.txt', { type: 'text/plain' })
    fireEvent.dragOver(page, { dataTransfer: { files: [file], types: ['Files'] } })
    fireEvent.drop(page, { dataTransfer: { files: [file], types: ['Files'] } })
    await waitFor(() => expect(uploadFile).toHaveBeenCalledWith(file, 'i1'))
  })

  it('has a single main landmark', async () => {
    serve(handlers())
    await openIssue()
    expect(document.querySelectorAll('main')).toHaveLength(1)
  })

  it('waits for a pasted file before posting', async () => {
    let finish!: (r: Awaited<ReturnType<typeof uploadFile>>) => void
    vi.mocked(uploadFile).mockReturnValue(new Promise((r) => { finish = r }))
    serve(handlers({ CreateComment: (v: Vars) => ({ createComment: { id: 'c9', body: v?.body } }) }))
    const activity = await openIssue()
    const box = within(activity).getByRole('textbox', { name: 'Comment' })
    await userEvent.type(box, 'See ')
    fireEvent.paste(box, { clipboardData: { files: [new File(['x'], 'a.png', { type: 'image/png' })], types: ['Files'], getData: () => '' } })
    await waitFor(() => expect(box).toHaveValue('See ![Uploading a.png…]()'))
    expect(within(activity).getByRole('button', { name: 'Comment' })).toBeDisabled()
    fireEvent.keyDown(box, { key: 'Enter', metaKey: true })
    expect(callsOf('CreateComment')).toHaveLength(0)
    finish({ id: 'a1', url: '/api/attachments/a1/a.png', name: 'a.png', size: 1, contentType: 'image/png', inline: true })
    await waitFor(() => expect(within(activity).getByRole('button', { name: 'Comment' })).toBeEnabled())
    fireEvent.keyDown(within(activity).getByRole('textbox', { name: 'Comment' }), { key: 'Enter', metaKey: true })
    await waitFor(() => expect(callsOf('CreateComment')).toEqual([{ issueId: 'i1', body: 'See ![a.png](/api/attachments/a1/a.png)', attachmentIds: ['a1'] }]))
  })
})
