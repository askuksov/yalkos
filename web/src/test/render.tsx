import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { DocumentNode, OperationDefinitionNode } from 'graphql'
import { createMemoryRouter, Outlet, RouterProvider } from 'react-router'

import { routes } from '../app/routes'
import { ToastProvider } from '../ui/Toast'
import { Where } from './Where'

export function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const router = createMemoryRouter(
    [
      {
        element: (
          <>
            <Outlet />
            <Where />
          </>
        ),
        children: routes,
      },
    ],
    { initialEntries: [path] },
  )
  const view = render(
    <QueryClientProvider client={client}>
      <ToastProvider>
        <RouterProvider router={router} />
      </ToastProvider>
    </QueryClientProvider>,
  )
  return { ...view, router, client }
}

/** Operation name of a typed document, used to route mocked requests. */
export function opName(doc: DocumentNode): string {
  const op = doc.definitions.find((d): d is OperationDefinitionNode => d.kind === 'OperationDefinition')
  return op?.name?.value ?? ''
}
