import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { member, owner, signedInAs } from '../../test/fixtures'
import { mocked, serve } from '../../test/serve'
import { Markdown } from '../issue-page/Markdown'
import { urlTransform } from './urlTransform'

vi.mock('../../api/client', async (orig) => {
  const actual = await orig<typeof import('../../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

function withProviders(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter([{ path: '*', element: node }])
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

const refIssue = { id: 'i12', identifier: 'YLK-12', title: 'Fix login', state: { id: 's-todo', name: 'Todo', type: 'UNSTARTED', color: '#888888' } }

describe('markdown', () => {
  it('links KEY-N outside code', async () => {
    serve({
      ...signedInAs(owner),
      IssueLink: (v) => {
        if (v?.id === 'YLK-12') return { issue: refIssue }
        throw new GraphQLRequestError('NOT_FOUND', 'Issue not found')
      },
    })
    const { container } = withProviders(<Markdown source={'see YLK-12 and `YLK-13`, also WEB-7\n\n```\nYLK-14\n```\n\n[YLK-15](https://example.com) xYLK-16'} />)
    const link = await screen.findByRole('link', { name: /YLK-12/ })
    expect(link).toHaveAttribute('href', '/issue/YLK-12')
    await waitFor(() => expect(link).toHaveAttribute('title', 'Fix login'))
    expect(container.querySelectorAll('a[href^="/issue/"]')).toHaveLength(1)
    // An identifier that does not resolve stays text.
    await waitFor(() => expect(screen.queryByRole('link', { name: 'WEB-7' })).toBeNull())
    expect(screen.getByText(/WEB-7/)).toBeInTheDocument()
    expect(container.querySelector('code')).toHaveTextContent('YLK-13')
  })

  it('renders mentions with the current name', async () => {
    serve(signedInAs(owner))
    withProviders(<Markdown source={`hi [@Old](mention:${member.id}) and [@Gone](mention:u-gone)`} />)
    expect(await screen.findByText('@Andrii')).toHaveAttribute('data-mention', member.id)
    expect(screen.getByText('@Gone')).toBeInTheDocument()
    expect(screen.queryByText('@Old')).toBeNull()
  })

  it('drops unsafe urls but keeps relative and mention', () => {
    expect(urlTransform('javascript:alert(1)')).toBe('')
    expect(urlTransform('JaVaScRiPt:alert(1)')).toBe('')
    expect(urlTransform('data:text/html;base64,xx')).toBe('')
    expect(urlTransform('vbscript:x')).toBe('')
    // Browsers drop tabs, newlines and leading control characters before reading the scheme.
    expect(urlTransform('java\tscript:alert(1)')).toBe('')
    expect(urlTransform('java\nscript:alert(1)')).toBe('')
    expect(urlTransform('\u0001javascript:alert(1)')).toBe('')
    expect(urlTransform(' javascript:alert(1)')).toBe('')
    expect(urlTransform('/a:b')).toBe('/a:b')
    expect(urlTransform('?q=a:b')).toBe('?q=a:b')
    expect(urlTransform('/api/attachments/a1/x.png')).toBe('/api/attachments/a1/x.png')
    expect(urlTransform('../x')).toBe('../x')
    expect(urlTransform('#top')).toBe('#top')
    expect(urlTransform('https://example.com/a')).toBe('https://example.com/a')
    expect(urlTransform('http://example.com')).toBe('http://example.com')
    expect(urlTransform('mailto:a@b.c')).toBe('mailto:a@b.c')
    expect(urlTransform('mention:u1')).toBe('mention:u1')
    const tabbed = render(<Markdown source={'[t](java&#9;script:alert(1))'} />)
    expect(tabbed.container.querySelector('a')?.getAttribute('href') ?? '').toBe('')
    tabbed.unmount()
    const { container } = render(<Markdown source={'[x](javascript:alert(1)) ![img](/api/attachments/a1/s.png) ![bad](data:image/png;base64,AA)'} />)
    expect(container.querySelector('a')?.getAttribute('href') ?? '').toBe('')
    const imgs = container.querySelectorAll('img')
    expect(imgs[0]).toHaveAttribute('src', '/api/attachments/a1/s.png')
    expect(imgs[1].getAttribute('src') ?? '').toBe('')
  })
})
