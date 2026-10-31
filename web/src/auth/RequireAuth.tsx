import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate, useLocation } from 'react-router'

import { GraphQLRequestError, request } from '../api/client'
import { SetupStatusQuery, ViewerQuery } from '../api/operations'

/** Redirects to /setup on a fresh instance and to /login without a session. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const location = useLocation()
  const setup = useQuery({ queryKey: ['setupStatus'], queryFn: () => request(SetupStatusQuery) })
  const viewer = useQuery({
    queryKey: ['viewer'],
    queryFn: () => request(ViewerQuery),
    enabled: setup.data?.setupStatus.needed === false,
  })

  if (setup.data?.setupStatus.needed) return <Navigate to="/setup" replace />
  if (viewer.error instanceof GraphQLRequestError && viewer.error.code === 'AUTHENTICATION_ERROR') {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  }
  if (setup.error || viewer.error) return <p role="alert">{t('status.loadFailed')}</p>
  if (!viewer.data) return <p className="page">{t('status.loading')}</p>
  return children
}
