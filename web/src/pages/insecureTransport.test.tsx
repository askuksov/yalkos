import { screen } from '@testing-library/react'
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
const plain = 'yal_api_' + 'd'.repeat(40)
const warning = /not encrypted/

/** Replaces window.location for one test; afterEach restores the real one. */
function at(href: string) {
  vi.stubGlobal('location', new URL(href))
}

// Explicit async timeout; the global default (1 s) is too tight on a slow run.
const slow = { timeout: 5000 }

async function openPage() {
  renderAt('/settings/api-keys')
  await screen.findByRole('heading', { name: 'Connect an AI agent' }, slow)
}

async function createKey() {
  const user = userEvent.setup()
  renderAt('/settings/api-keys')
  await user.type(await screen.findByLabelText('Key name', undefined, slow), 'Agent')
  await user.click(screen.getByRole('button', { name: 'Create key' }))
  await screen.findByText(/You will not see this key again/, undefined, slow)
}

beforeEach(() => {
  vi.mocked(request).mockReset()
  serve({
    Projects: () => ({ projects: { nodes: [] } }),
    SetupStatus: () => ({ setupStatus: { needed: false } }),
    Viewer: () => ({ viewer: owner }),
    ApiKeys: () => ({ apiKeys: [] }),
    CreateApiKey: () => ({ createApiKey: { key: plain, apiKey: { id: 'k1' } } }),
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('plain HTTP notice on the API keys page', () => {
  it('is shown on http with a remote host', async () => {
    at('http://yalkos.example.com/settings/api-keys')
    await openPage()
    const note = screen.getByRole('note')
    expect(note).toHaveTextContent(warning)
    expect(note).toHaveTextContent(/anyone on the network can read it/)
    expect(note).toHaveTextContent(/HTTPS/)
  })

  it('is shown on http with a private network address', async () => {
    at('http://192.168.1.10:8080/settings/api-keys')
    await openPage()
    expect(screen.getByRole('note')).toHaveTextContent(warning)
  })

  it('is not an alert', async () => {
    at('http://yalkos.example.com/settings/api-keys')
    await openPage()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('is shown once even though the agent setup is on the same page', async () => {
    at('http://yalkos.example.com/settings/api-keys')
    await openPage()
    expect(screen.getAllByRole('note')).toHaveLength(1)
    expect(screen.getAllByText(warning)).toHaveLength(1)
  })

  it('is still shown once after a key is created', async () => {
    at('http://yalkos.example.com/settings/api-keys')
    await createKey()
    expect(screen.getAllByRole('note')).toHaveLength(1)
  })

  it('is not shown on https with a remote host', async () => {
    at('https://yalkos.example.com/settings/api-keys')
    await openPage()
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
  })

  it.each(['http://localhost:5173', 'http://127.0.0.1:8080', 'http://[::1]:8080', 'http://app.localhost'])(
    'is not shown on loopback %s',
    async (origin) => {
      at(origin + '/settings/api-keys')
      await openPage()
      expect(screen.queryByRole('note')).not.toBeInTheDocument()
    },
  )

  it('is not shown with the default test location', async () => {
    await openPage()
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
  })

  it('does not mention the key value', async () => {
    at('http://yalkos.example.com/settings/api-keys')
    await createKey()
    expect(screen.getByRole('note')).not.toHaveTextContent(plain)
  })
})
