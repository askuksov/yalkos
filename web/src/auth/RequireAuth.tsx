import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'

import { GraphQLRequestError, request } from '../api/client'
import { SetupStatusQuery, ViewerQuery } from '../api/operations'

/** Redirects to /setup on a fresh instance and to /login without a session. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const location = useLocation()
  const setup = useQuery({ queryKey: ['setupStatus'], queryFn: () => request(SetupStatusQuery) })
  const viewer = useQuery({
    queryKey: ['viewer'],
    queryFn: () => request(ViewerQuery),
    enabled: setup.data?.setupStatus.needed === false,
  })

  if (setup.data?.setupStatus.needed) return <Navigate to="/setup" replace />
  if (viewer.error instanceof GraphQLRequestError && viewer.error.code === 'AUTHENTICATION_ERROR') {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  if (setup.error || viewer.error) return <p role="alert">Could not load. Reload the page.</p>
  if (!viewer.data) return <p>Loading…</p>
  return children
}
