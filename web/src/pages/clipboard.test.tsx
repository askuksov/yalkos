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
    return screen.getByRole('button', { name: 'Copy Claude Code command' })
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
    await user.click(screen.getByRole('button', { name: 'Copy Claude Code command' }))
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
  })
})

describe('setup snippet failure and key changes', () => {
  const copySnippet = () => screen.getByRole('button', { name: 'Copy Claude Code command' })
  const createAnother = async (user: ReturnType<typeof userEvent.setup>) => {
    await user.type(await screen.findByLabelText('Key name'), 'Second')
    await user.click(screen.getByRole('button', { name: 'Create key' }))
    await screen.findByText(/You will not see this key again/)
  }

  it('drops the snippet failure when Done hides the key', async () => {
    const { user } = session('rejects')
    stubExecCommand(false)
    await createKey(user)
    await user.click(copySnippet())
    expect(await screen.findByRole('status')).toHaveTextContent(failureText)
    await user.click(screen.getByRole('button', { name: 'Done' }))
    await screen.findByLabelText('Key name')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('drops the snippet failure when a key is created', async () => {
    const { user } = session('rejects')
    stubExecCommand(false)
    renderAt('/settings/api-keys')
    await screen.findByRole('heading', { name: 'Connect an AI agent' })
    await user.click(copySnippet())
    expect(await screen.findByRole('status')).toHaveTextContent(failureText)
    await createAnother(user)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('drops the snippet failure when a second key is created', async () => {
    const { user } = session('rejects')
    stubExecCommand(false)
    await createKey(user)
    await user.click(copySnippet())
    expect(await screen.findByRole('status')).toHaveTextContent(failureText)
    await user.click(screen.getByRole('button', { name: 'Done' }))
    await createAnother(user)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})

describe('new API key copy', () => {
  const copyKey = () => screen.getByRole('button', { name: 'Copy new API key' })

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

describe('copy button names', () => {
  const names = {
    key: 'Copy new API key',
    claude: 'Copy Claude Code command',
    openCode: 'Copy OpenCode configuration',
    env: 'Copy environment variable export',
  }
  const button = (name: string) => screen.getByRole('button', { name })

  it('names the new key button', async () => {
    const { user } = session('works')
    await createKey(user)
    expect(button(names.key)).toHaveTextContent('Copy')
  })

  it('names the Claude Code button', async () => {
    const { user } = session('works')
    await createKey(user)
    expect(button(names.claude)).toHaveTextContent('Copy')
  })

  it('names the OpenCode button', async () => {
    const { user } = session('works')
    await createKey(user)
    expect(button(names.openCode)).toHaveTextContent('Copy')
  })

  it('names the environment variable button', async () => {
    const { user } = session('works')
    await createKey(user)
    expect(button(names.env)).toHaveTextContent('Copy')
  })

  it('copies the new key', async () => {
    const { user, writeText } = session('works')
    await createKey(user)
    await user.click(button(names.key))
    expect(writeText).toHaveBeenCalledExactlyOnceWith(plain)
  })

  it('copies the Claude Code command', async () => {
    const { user, writeText } = session('works')
    await createKey(user)
    await user.click(button(names.claude))
    expect(writeText).toHaveBeenCalledExactlyOnceWith(claudeCommand(plain))
  })

  it('copies the OpenCode configuration', async () => {
    const { user, writeText } = session('works')
    await createKey(user)
    await user.click(button(names.openCode))
    expect(writeText).toHaveBeenCalledTimes(1)
    const text = writeText?.mock.calls[0][0] as string
    expect(JSON.parse(text)).toEqual({
      mcp: {
        yalkos: {
          type: 'remote',
          url: `${window.location.origin}/mcp`,
          headers: { Authorization: 'Bearer {env:YALKOS_API_KEY}' },
          oauth: false,
        },
      },
    })
    expect(text).not.toContain(plain)
  })

  it('copies the environment variable export', async () => {
    const { user, writeText } = session('works')
    await createKey(user)
    await user.click(button(names.env))
    expect(writeText).toHaveBeenCalledExactlyOnceWith(`export YALKOS_API_KEY=${plain}`)
  })

  it('gives the four buttons four distinct names', async () => {
    const { user } = session('works')
    await createKey(user)
    const labels = screen.getAllByRole('button', { name: /^Copy/ }).map((b) => b.getAttribute('aria-label'))
    expect(labels).toEqual([names.key, names.claude, names.openCode, names.env])
    expect(new Set(labels).size).toBe(4)
  })

  it('keeps the key value out of every button name', async () => {
    const { user } = session('works')
    await createKey(user)
    for (const b of screen.getAllByRole('button')) {
      expect(b.getAttribute('aria-label') ?? '').not.toContain(plain)
      expect(b.getAttribute('title') ?? '').not.toContain(plain)
    }
    expect(screen.queryByRole('button', { name: new RegExp(plain) })).not.toBeInTheDocument()
  })
})
