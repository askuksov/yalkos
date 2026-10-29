import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { DocumentNode, OperationDefinitionNode } from 'graphql'
import { MemoryRouter, Route, Routes } from 'react-router'

import { AppRoutes } from '../App'
import { Where } from './Where'

export function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <AppRoutes />
        <Routes>
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

/** Operation name of a typed document, used to route mocked requests. */
export function opName(doc: DocumentNode): string {
  const op = doc.definitions.find((d): d is OperationDefinitionNode => d.kind === 'OperationDefinition')
  return op?.name?.value ?? ''
}
