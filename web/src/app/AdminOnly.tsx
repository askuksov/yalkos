import type { ReactNode } from 'react'

import { useIsAdmin } from '../api/hooks'
import { NotFound } from './ProjectRoute'

/**
 * Renders its children for owners and admins only. Others get the not-found page:
 * the UI has no forbidden state, and the API refuses their requests anyway.
 */
export function AdminOnly({ children }: { children: ReactNode }) {
  const isAdmin = useIsAdmin()
  return isAdmin ? children : <NotFound what="page" />
}
