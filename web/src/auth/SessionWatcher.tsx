import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router'

import { GraphQLRequestError } from '../api/client'

/** Pages that work without a session: an auth error there is a form error. */
function isPublic(path: string): boolean {
  return path === '/login' || path === '/setup' || path.startsWith('/invite/')
}

/**
 * Sends the user to /login when any query or mutation reports that the
 * session is gone (expired, revoked, signed out elsewhere). Renders nothing.
 */
export function SessionWatcher() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  useEffect(() => {
    if (isPublic(pathname)) return
    const onError = (err: unknown) => {
      if (!(err instanceof GraphQLRequestError) || err.code !== 'AUTHENTICATION_ERROR') return
      qc.clear()
      navigate('/login', { replace: true, state: { from: pathname } })
    }
    const offQueries = qc.getQueryCache().subscribe((e) => {
      if (e.type === 'updated' && e.action.type === 'error') onError(e.action.error)
    })
    const offMutations = qc.getMutationCache().subscribe((e) => {
      if (e.type === 'updated' && e.action.type === 'error') onError(e.action.error)
    })
    return () => {
      offQueries()
      offMutations()
    }
  }, [qc, navigate, pathname])

  return null
}
