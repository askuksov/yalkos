import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { owner, signedInAs } from '../../test/fixtures'
import { deferred, mocked, serve } from '../../test/serve'
import { ToastProvider } from '../../ui/Toast'
import { MarkdownEditor } from './MarkdownEditor'
import type { UploadResult } from './upload'
import { uploadFile } from './upload'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})
vi.mock('./upload', async (orig) => {
  const actual = await orig<typeof import('./upload')>()
  return { ...actual, uploadFile: vi.fn() }
})
const upload = vi.mocked(uploadFile)

beforeEach(() => {
  mocked.mockReset()
  upload.mockReset()
  serve({ ...signedInAs(owner), Limits: () => ({ limits: { maxUploadMb: 25 } }) })
})

let lastValue = ''
let reported: string[] = []
let busy: boolean[] = []

function Harness({ issueId, initial = '' }: { issueId?: string; initial?: string }) {
  const [value, setValue] = useState(initial)
  return (
    <MarkdownEditor
      label="Description"
      value={value}
      onChange={(v) => {
        lastValue = v
        setValue(v)
      }}
      issueId={issueId}
      onAttachmentsChange={(ids) => {
        reported = ids
      }}
      onBusyChange={(b) => {
        busy.push(b)
      }}
      placeholder="Write something"
    />
  )
}

function mount(props: { issueId?: string; initial?: string } = {}) {
  lastValue = props.initial ?? ''
  reported = []
  busy = []
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ToastProvider>
        <Harness {...props} />
      </ToastProvider>
    </QueryClientProvider>,
  )
}

function paste(target: HTMLElement, files: File[]) {
  fireEvent.paste(target, { clipboardData: { files, types: ['Files'], getData: () => '' } })
}

const png = (name = 'shot.png', size = 10) => new File([new Uint8Array(size)], name, { type: 'image/png' })
const result = (over: Partial<UploadResult> = {}): UploadResult => ({
  id: 'a1', url: '/api/attachments/a1/shot.png', name: 'shot.png', size: 10, contentType: 'image/png', inline: true, ...over,
})

describe('MarkdownEditor', () => {
  it('pasting an image uploads and inserts markdown', async () => {
    const pending = deferred<UploadResult>()
    upload.mockReturnValue(pending.promise)
    mount({ issueId: 'i1', initial: 'see ' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    await waitFor(() => expect(mocked).toHaveBeenCalled())
    area.focus()
    ;(area as HTMLTextAreaElement).setSelectionRange(4, 4)
    paste(area, [png()])
    await waitFor(() => expect(lastValue).toBe('see ![Uploading shot.png…]()'))
    expect(upload).toHaveBeenCalledWith(expect.any(File), 'i1')
    pending.resolve(result())
    await waitFor(() => expect(lastValue).toBe('see ![shot.png](/api/attachments/a1/shot.png)'))
  })

  it('inserts a link for files that are not shown inline', async () => {
    upload.mockResolvedValue(result({ id: 'a2', url: '/api/attachments/a2/notes.txt', name: 'notes.txt', contentType: 'text/plain; charset=utf-8', inline: false }))
    mount({ issueId: 'i1' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    fireEvent.drop(area, { dataTransfer: { files: [new File(['x'], 'notes.txt')], types: ['Files'] } })
    await waitFor(() => expect(lastValue).toBe('[notes.txt](/api/attachments/a2/notes.txt)'))
  })

  it('too large file shows the limit and no placeholder', async () => {
    mount({ issueId: 'i1' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    await waitFor(() => expect(mocked).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 0))
    paste(area, [png('big.png', 30 * 1024 * 1024)])
    expect(await screen.findByText('File is larger than 25 MB')).toBeInTheDocument()
    expect(upload).not.toHaveBeenCalled()
    expect(lastValue).toBe('')
  })

  it('a failed upload removes the placeholder and explains', async () => {
    const { GraphQLRequestError } = await import('../../api/client')
    upload.mockRejectedValue(new GraphQLRequestError('INVALID_INPUT', 'too large', { reason: 'too_large', params: { maxMb: 25 } }))
    mount({ issueId: 'i1', initial: 'x' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    paste(area, [png()])
    expect(await screen.findByText('File is larger than 25 MB')).toBeInTheDocument()
    expect(lastValue).toBe('x')
  })

  it('@ inserts a mention', async () => {
    mount({ initial: 'ping ' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    await userEvent.click(area)
    await userEvent.type(area, '{End}@')
    const picker = await screen.findByRole('combobox')
    await userEvent.type(picker, 'And')
    await userEvent.click(within(screen.getByRole('listbox')).getByText('Andrii'))
    await waitFor(() => expect(lastValue).toBe('ping [@Andrii](mention:u2) '))
    expect(screen.queryByRole('combobox')).toBeNull()
  })

  it('reports uploaded ids for pending attachments', async () => {
    upload.mockResolvedValueOnce(result()).mockResolvedValueOnce(result({ id: 'a2', url: '/api/attachments/a2/b.png', name: 'b.png' }))
    mount()
    const area = await screen.findByRole('textbox', { name: 'Description' })
    paste(area, [png(), png('b.png')])
    await waitFor(() => expect(reported).toEqual(['a1', 'a2']))
    expect(upload).toHaveBeenCalledWith(expect.any(File), undefined)
    expect(lastValue).toContain('![shot.png](/api/attachments/a1/shot.png)')
    expect(lastValue).toContain('![b.png](/api/attachments/a2/b.png)')
  })

  it('previews markdown', async () => {
    mount({ initial: '**bold**' })
    await userEvent.click(await screen.findByRole('tab', { name: 'Preview' }))
    expect(screen.getByText('bold').tagName).toBe('STRONG')
    await userEvent.click(screen.getByRole('tab', { name: 'Write' }))
    expect(screen.getByRole('textbox', { name: 'Description' })).toHaveValue('**bold**')
  })

  it('reports uploads in flight', async () => {
    const pending = deferred<UploadResult>()
    upload.mockReturnValue(pending.promise)
    mount({ issueId: 'i1' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    paste(area, [png()])
    await waitFor(() => expect(busy).toEqual([true]))
    pending.resolve(result())
    await waitFor(() => expect(busy).toEqual([true, false]))
  })

  it('ignores an upload that finishes after the editor is gone', async () => {
    const pending = deferred<UploadResult>()
    upload.mockReturnValue(pending.promise)
    const view = mount({ issueId: 'i1', initial: 'x' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    paste(area, [png()])
    await waitFor(() => expect(lastValue).toContain('Uploading'))
    view.unmount()
    lastValue = 'after'
    pending.resolve(result())
    await new Promise((r) => setTimeout(r, 10))
    expect(lastValue).toBe('after')
  })

  it('reports not busy when it closes during an upload', async () => {
    upload.mockReturnValue(new Promise(() => {}))
    const view = mount({ issueId: 'i1' })
    const area = await screen.findByRole('textbox', { name: 'Description' })
    paste(area, [png()])
    await waitFor(() => expect(busy).toEqual([true]))
    view.unmount()
    expect(busy).toEqual([true, false])
  })
})
