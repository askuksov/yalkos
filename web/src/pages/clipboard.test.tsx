import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { request } from '../api/client'
import { renderAt } from '../test/render'
import { serve } from '../test/serve'

vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>()
  return { ...actual, request: vi.fn() }
})

const owner = { id: 'u1', name: 'Olga', email: 'o@example.com', role: 'OWNER' }
const plain = 'yal_api_' + 'c'.repeat(40)
const failureText = 'Copy failed: select the text and copy it manually.'
const claudeCommand = (key: string) =>
  `claude mcp add --transport http yalkos ${window.location.origin}/mcp --header "Authorization: Bearer ${key}"`

const page = () =>
  serve({
    Projects: () => ({ projects: { nodes: [] } }),
    SetupStatus: () => ({ setupStatus: { needed: false } }),
    Viewer: () => ({ viewer: owner }),
    ApiKeys: () => ({ apiKeys: [] }),
    CreateApiKey: () => ({ createApiKey: { key: plain, apiKey: { id: 'k1' } } }),
  })

/** Text and selection of the temporary textarea at the moment execCommand runs. */
type Seen = { text: string; selected: string }

function stubExecCommand(result: boolean) {
  const seen: Seen[] = []
  const exec = vi.fn((command: string) => {
    const area = document.querySelector('textarea')
    if (command === 'copy' && area) {
      seen.push({ text: area.value, selected: area.value.slice(area.selectionStart, area.selectionEnd) })
    }
    return result
  })
  Object.defineProperty(document, 'execCommand', { value: exec, configurable: true, writable: true })
  return { exec, seen }
}

/** A user session whose clipboard API behaves as given. */
function session(clipboard: 'works' | 'missing' | 'rejects') {
  const user = userEvent.setup()
  let writeText: ReturnType<typeof vi.fn> | undefined
  if (clipboard === 'missing') {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
  } else {
    writeText = vi.fn(clipboard === 'works' ? async () => undefined : async () => Promise.reject(new Error('denied')))
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  }
  return { user, writeText }
}

async function createKey(user: ReturnType<typeof userEvent.setup>) {
  renderAt('/settings/api-keys')
  await user.type(await screen.findByLabelText('Key name'), 'Agent')
  await user.click(screen.getByRole('button', { name: 'Create key' }))
  await screen.findByText(/You will not see this key again/)
}

beforeEach(() => {
  vi.mocked(request).mockReset()
  page()
})

afterEach(() => {
  Reflect.deleteProperty(document, 'execCommand')
  Reflect.deleteProperty(navigator, 'clipboard')
})

describe('setup snippet copy', () => {
  const open = async () => {
    renderAt('/settings/api-keys')
    await screen.findByRole('heading', { name: 'Connect an AI agent' })
    return screen.getAllByRole('button', { name: 'Copy' })[0]
  }

  it('writes the snippet through the clipboard API', async () => {
    const { user, writeText } = session('works')
    const { exec } = stubExecCommand(true)
    await user.click(await open())
    expect(writeText).toHaveBeenCalledExactlyOnceWith(claudeCommand('YOUR_API_KEY'))
    expect(exec).not.toHaveBeenCalled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('copies through a temporary textarea when the clipboard API is missing', async () => {
    const { user } = session('missing')
    const { seen } = stubExecCommand(true)
    await user.click(await open())
    expect(seen).toEqual([{ text: claudeCommand('YOUR_API_KEY'), selected: claudeCommand('YOUR_API_KEY') }])
    expect(document.querySelector('textarea')).toBeNull()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('copies through a temporary textarea when writeText rejects', async () => {
    const { user, writeText } = session('rejects')
    const { seen } = stubExecCommand(true)
    await user.click(await open())
    expect(writeText).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(seen).toHaveLength(1))
    expect(seen[0].text).toBe(claudeCommand('YOUR_API_KEY'))
    expect(document.querySelector('textarea')).toBeNull()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows a status when every way of copying fails', async () => {
    const { user } = session('rejects')
    stubExecCommand(false)
    await user.click(await open())
    expect(await screen.findByRole('status')).toHaveTextContent(failureText)
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('shows a status when execCommand is not available', async () => {
    const { user } = session('missing')
    await user.click(await open())
    expect(await screen.findByRole('status')).toHaveTextContent(failureText)
  })

  it('clears the status after a later copy succeeds', async () => {
    const { user, writeText } = session('rejects')
    stubExecCommand(false)
    await user.click(await open())
    expect(await screen.findByRole('status')).toBeInTheDocument()
    writeText?.mockResolvedValue(undefined)
    await user.click(screen.getAllByRole('button', { name: 'Copy' })[0])
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
  })
})

describe('new API key copy', () => {
  // The key section comes first on the page, so its button is the first Copy.
  const copyKey = () => screen.getAllByRole('button', { name: 'Copy' })[0]

  it('writes the key through the clipboard API', async () => {
    const { user, writeText } = session('works')
    const { exec } = stubExecCommand(true)
    await createKey(user)
    await user.click(copyKey())
    expect(writeText).toHaveBeenCalledExactlyOnceWith(plain)
    expect(exec).not.toHaveBeenCalled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('copies the key through a temporary textarea when the clipboard API is missing', async () => {
    const { user } = session('missing')
    const { seen } = stubExecCommand(true)
    await createKey(user)
    await user.click(copyKey())
    expect(seen).toEqual([{ text: plain, selected: plain }])
    expect(document.querySelector('textarea')).toBeNull()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('copies the key through a temporary textarea when writeText rejects', async () => {
    const { user, writeText } = session('rejects')
    const { seen } = stubExecCommand(true)
    await createKey(user)
    await user.click(copyKey())
    expect(writeText).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(seen).toHaveLength(1))
    expect(seen[0].text).toBe(plain)
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('shows a status when every way of copying fails and keeps the key selectable', async () => {
    const { user } = session('rejects')
    stubExecCommand(false)
    await createKey(user)
    await user.click(copyKey())
    expect(await screen.findByRole('status')).toHaveTextContent(failureText)
    expect(screen.getAllByText(plain).length).toBeGreaterThan(0)
  })

  it('does not carry a failure over to the next key', async () => {
    const { user } = session('rejects')
    stubExecCommand(false)
    await createKey(user)
    await user.click(copyKey())
    expect(await screen.findByRole('status')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Done' }))
    await user.type(await screen.findByLabelText('Key name'), 'Second')
    await user.click(screen.getByRole('button', { name: 'Create key' }))
    await screen.findByText(/You will not see this key again/)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('never puts the key into an accessible name', async () => {
    const { user } = session('works')
    await createKey(user)
    const labelled = document.querySelectorAll('[aria-label], [aria-labelledby], [title]')
    for (const el of labelled) {
      expect(el.getAttribute('aria-label') ?? '').not.toContain(plain)
      expect(el.getAttribute('title') ?? '').not.toContain(plain)
    }
    expect(screen.getAllByRole('button').every((b) => !(b.getAttribute('aria-label') ?? '').includes(plain))).toBe(true)
  })
})
