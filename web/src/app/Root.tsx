import { Outlet } from 'react-router'

import { SessionWatcher } from '../auth/SessionWatcher'

/** Top route: watches for a lost session on every page. */
export function Root() {
  return (
    <>
      <SessionWatcher />
      <Outlet />
    </>
  )
}
