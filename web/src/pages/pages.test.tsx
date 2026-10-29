import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { DocumentNode } from 'graphql'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError, request } from '../api/client'
import { opName, renderAt } from '../test/render'

vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>()
  return { ...actual, request: vi.fn() }
})

const mocked = vi.mocked(request)
type Handler = (vars: Record<string, unknown> | undefined) => unknown

function serve(handlers: Record<string, Handler>) {
  mocked.mockImplementation(async (doc, vars) => {
    const h = handlers[opName(doc as unknown as DocumentNode)]
    if (!h) throw new Error(`unexpected operation ${opName(doc as unknown as DocumentNode)}`)
    return h(vars as Record<string, unknown> | undefined) as never
  })
}

const signedOut = {
  SetupStatus: () => ({ setupStatus: { needed: false } }),
  Viewer: () => {
    throw new GraphQLRequestError('AUTHENTICATION_ERROR', 'Authentication required')
  },
}
const owner = { id: 'u1', name: 'Olga', email: 'o@example.com', role: 'OWNER' }

beforeEach(() => {
  mocked.mockReset()
})

describe('LoginPage', () => {
  it('submits and goes home', async () => {
    let loggedIn = false
    serve({
      ...signedOut,
      Viewer: () => {
        if (!loggedIn) throw new GraphQLRequestError('AUTHENTICATION_ERROR', 'Authentication required')
        return { viewer: owner }
      },
      Login: (vars) => {
        expect(vars).toEqual({ email: 'o@example.com', password: 'owner-password' })
        loggedIn = true
        return { login: { id: 'u1' } }
      },
    })
    renderAt('/login')
    await userEvent.type(await screen.findByLabelText('Email'), 'o@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'owner-password')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/$/))
    expect(await screen.findByText('Signed in as Olga (owner)')).toBeInTheDocument()
  })

  it('shows invalid credentials message', async () => {
    serve({
      ...signedOut,
      Login: () => {
        throw new GraphQLRequestError('AUTHENTICATION_ERROR', 'Invalid email or password')
      },
    })
    renderAt('/login')
    await userEvent.type(await screen.findByLabelText('Email'), 'o@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'bad-password')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('Invalid email or password')).toBeInTheDocument()
  })

  it('shows rate limit message', async () => {
    serve({
      ...signedOut,
      Login: () => {
        throw new GraphQLRequestError('RATE_LIMITED', 'whatever')
      },
    })
    renderAt('/login')
    await userEvent.type(await screen.findByLabelText('Email'), 'o@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'bad-password')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('Too many attempts. Try again in a few minutes.')).toBeInTheDocument()
  })
})

describe('SetupPage', () => {
  it('prefills code from query string', async () => {
    serve({ SetupStatus: () => ({ setupStatus: { needed: true } }) })
    renderAt('/setup?code=ABCD2345')
    expect(await screen.findByLabelText('Setup code')).toHaveValue('ABCD2345')
  })
})

describe('RequireAuth', () => {
  it('redirects to /setup when needed', async () => {
    serve({ SetupStatus: () => ({ setupStatus: { needed: true } }) })
    renderAt('/')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/setup'))
  })

  it('redirects to /login when unauthenticated', async () => {
    serve(signedOut)
    renderAt('/settings/api-keys')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/login'))
  })
})

describe('ApiKeysPage', () => {
  it('shows a new key once', async () => {
    const plain = 'yal_api_' + 'a'.repeat(40)
    serve({
      SetupStatus: () => ({ setupStatus: { needed: false } }),
      Viewer: () => ({ viewer: owner }),
      ApiKeys: () => ({
        apiKeys: [{ id: 'k1', name: 'CI', displayPrefix: 'yal_api_aaaa…', createdAt: '2026-10-30T09:05:00Z', lastUsedAt: null }],
      }),
      CreateApiKey: (vars) => {
        expect(vars).toEqual({ name: 'CI' })
        return { createApiKey: { key: plain, apiKey: { id: 'k1' } } }
      },
    })
    renderAt('/settings/api-keys')
    await userEvent.type(await screen.findByLabelText('Key name'), 'CI')
    await userEvent.click(screen.getByRole('button', { name: 'Create key' }))
    expect(await screen.findByText(plain)).toBeInTheDocument()
    expect(screen.getByText(/You will not see this key again/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(screen.queryByText(plain)).not.toBeInTheDocument()
    expect(screen.getByText('yal_api_aaaa…')).toBeInTheDocument()
  })
})

describe('InvitePage', () => {
  it('shows invalid link message', async () => {
    serve({
      SetupStatus: () => ({ setupStatus: { needed: false } }),
      AcceptInvite: () => {
        throw new GraphQLRequestError('INVITE_INVALID', 'x')
      },
    })
    renderAt('/invite/tok123')
    await userEvent.type(await screen.findByLabelText('Name'), 'Mia')
    await userEvent.type(screen.getByLabelText('Email'), 'mia@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'mia-password')
    await userEvent.click(screen.getByRole('button', { name: 'Join workspace' }))
    expect(await screen.findByText('This invite link is invalid or has expired.')).toBeInTheDocument()
  })
})

describe('setup flow', () => {
  it('lands on home after setup instead of bouncing back to /setup', async () => {
    let done = false
    serve({
      SetupStatus: () => ({ setupStatus: { needed: !done } }),
      Viewer: () => {
        if (!done) throw new GraphQLRequestError('AUTHENTICATION_ERROR', 'Authentication required')
        return { viewer: owner }
      },
      Setup: () => {
        done = true
        return { setup: { id: 'u1' } }
      },
    })
    renderAt('/')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/setup'))
    await userEvent.type(screen.getByLabelText('Setup code'), 'ABCD2345')
    await userEvent.type(screen.getByLabelText('Workspace name'), 'Acme')
    await userEvent.type(screen.getByLabelText('Your name'), 'Olga')
    await userEvent.type(screen.getByLabelText('Email'), 'o@example.com')
    await userEvent.type(screen.getByLabelText('Password'), 'owner-password')
    await userEvent.click(screen.getByRole('button', { name: 'Create workspace' }))
    expect(await screen.findByText('Signed in as Olga (owner)')).toBeInTheDocument()
    expect(screen.getByTestId('where')).toHaveTextContent(/^\/$/)
  })
})

describe('client', () => {
  it('turns a non-GraphQL error body into a readable error', async () => {
    const { request: realRequest } = await vi.importActual<typeof import('../api/client')>('../api/client')
    const { ViewerQuery } = await import('../api/operations')
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"request origin does not match BASE_URL"}', { status: 403 })))
    await expect(realRequest(ViewerQuery)).rejects.toMatchObject({ message: 'request origin does not match BASE_URL' })
    vi.unstubAllGlobals()
  })
})
