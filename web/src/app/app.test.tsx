import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { errorMessage, GraphQLRequestError } from '../api/client'
import { member, owner, project, signedInAs } from '../test/fixtures'
import { renderAt } from '../test/render'
import { mocked, serve } from '../test/serve'

vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

describe('app shell', () => {
  it('redirects / to /my-issues', async () => {
    serve(signedInAs(owner))
    renderAt('/')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/my-issues$/))
    expect(await screen.findByRole('navigation', { name: 'Main' })).toBeInTheDocument()
  })

  it('lists only member projects in the sidebar', async () => {
    serve(signedInAs(owner, [project(), project({ id: 'p2', key: 'WEB', name: 'Website', isMember: false })]))
    renderAt('/my-issues')
    const nav = await screen.findByRole('navigation', { name: 'Main' })
    expect(await within(nav).findByRole('link', { name: /Yalkos/ })).toHaveAttribute('href', '/p/YLK')
    expect(within(nav).queryByRole('link', { name: /Website/ })).not.toBeInTheDocument()
    expect(within(nav).getByRole('link', { name: 'Browse projects' })).toHaveAttribute('href', '/projects')
  })

  it('redirects a former project key', async () => {
    // The server resolves the former key YLK to the project now keyed YK.
    serve({ ...signedInAs(owner, [project({ key: 'YK' })]), Project: () => ({ project: project({ key: 'YK' }) }) })
    renderAt('/p/YLK/settings')
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/YK\/settings$/))
  })

  it('shows project not found', async () => {
    serve(signedInAs(owner))
    renderAt('/p/NOPE')
    expect(await screen.findByText('Project not found')).toBeInTheDocument()
  })

  it('hides project settings link for members', async () => {
    serve(signedInAs(member))
    const { unmount } = renderAt('/p/YLK')
    const nav = await screen.findByRole('navigation', { name: 'Main' })
    expect(await within(nav).findByRole('link', { name: 'Issues' })).toBeInTheDocument()
    expect(within(nav).queryByRole('link', { name: 'Settings' })).not.toBeInTheDocument()
    unmount()

    serve(signedInAs(owner))
    renderAt('/p/YLK')
    const nav2 = await screen.findByRole('navigation', { name: 'Main' })
    expect(await within(nav2).findByRole('link', { name: 'Settings' })).toHaveAttribute('href', '/p/YLK/settings')
  })

  it('collapses the sidebar with [', async () => {
    serve(signedInAs(owner))
    renderAt('/my-issues')
    await screen.findByRole('navigation', { name: 'Main' })
    await userEvent.keyboard('[[')
    expect(screen.queryByRole('navigation', { name: 'Main' })).not.toBeInTheDocument()
    expect(localStorage.getItem('yalkos.sidebar')).toBe('collapsed')
    await userEvent.keyboard('[[')
    expect(screen.getByRole('navigation', { name: 'Main' })).toBeInTheDocument()
  })

  it('switches the theme from the user menu', async () => {
    serve(signedInAs(owner))
    renderAt('/my-issues')
    await userEvent.click(await screen.findByRole('button', { name: /Olga/ }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Dark/ }))
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('yalkos.theme')).toBe('dark')
  })
})

describe('error text', () => {
  it('shows translated error text by code', () => {
    expect(errorMessage(new GraphQLRequestError('RATE_LIMITED', 'server text'))).toBe('Too many attempts. Try again in a few minutes.')
    expect(errorMessage(new GraphQLRequestError('SOMETHING_NEW', 'Server says hi'))).toBe('Server says hi')
    expect(errorMessage(new Error('boom'))).toBe('Something went wrong. Try again.')
  })

  it('translates field errors by reason with params', () => {
    const tooLong = new GraphQLRequestError('INVALID_INPUT', 'title must be at most 255 characters', {
      field: 'title', reason: 'too_long', params: { max: 255 },
    })
    expect(errorMessage(tooLong)).toBe('Must be at most 255 characters')
    const odd = new GraphQLRequestError('INVALID_INPUT', 'odd server text', { field: 'x', reason: 'brand_new' })
    expect(errorMessage(odd)).toBe('odd server text')
  })
})
