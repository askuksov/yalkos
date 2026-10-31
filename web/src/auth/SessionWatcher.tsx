import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router'

import { GraphQLRequestError } from '../api/client'
import { sessionGeneration, startSession } from './session'

/** Pages that work without a session: an auth error there is a form error. */
function isPublic(path: string): boolean {
  return path === '/login' || path === '/setup' || path.startsWith('/invite/')
}

/** Generation each mutation started under; kept across subscriptions. */
const startedUnder = new WeakMap<object, number>()

/**
 * Sends the user to /login when any query or mutation reports that the
 * session is gone (expired, revoked, signed out elsewhere). An auth error from
 * a mutation that started under an earlier session (before a new sign-in) is
 * ignored: it says nothing about the current session. Renders nothing.
 */
export function SessionWatcher() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  // Record start generations on every path, public ones included.
  useEffect(
    () =>
      qc.getMutationCache().subscribe((e) => {
        if (e.type === 'added') startedUnder.set(e.mutation, sessionGeneration())
      }),
    [qc],
  )

  useEffect(() => {
    if (isPublic(pathname)) return
    const onError = (err: unknown) => {
      if (!(err instanceof GraphQLRequestError) || err.code !== 'AUTHENTICATION_ERROR') return
      startSession()
      qc.clear()
      navigate('/login', { replace: true, state: { from: pathname } })
    }
    const offQueries = qc.getQueryCache().subscribe((e) => {
      if (e.type === 'updated' && e.action.type === 'error') onError(e.action.error)
    })
    const offMutations = qc.getMutationCache().subscribe((e) => {
      if (e.type !== 'updated' || e.action.type !== 'error') return
      const started = startedUnder.get(e.mutation)
      if (started !== undefined && started < sessionGeneration()) return
      onError(e.action.error)
    })
    return () => {
      offQueries()
      offMutations()
    }
  }, [qc, navigate, pathname])

  return null
}
