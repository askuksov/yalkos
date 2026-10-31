import { Navigate, type RouteObject } from 'react-router'

import { RequireAuth } from '../auth/RequireAuth'
import { ApiKeysPage } from '../pages/ApiKeysPage'
import { InvitePage } from '../pages/InvitePage'
import { IssuePage } from '../pages/IssuePage'
import { LoginPage } from '../pages/LoginPage'
import { MembersPage } from '../pages/MembersPage'
import { MyIssuesPage } from '../pages/MyIssuesPage'
import { ProjectBoardPage } from '../pages/ProjectBoardPage'
import { ProjectIssuesPage } from '../pages/ProjectIssuesPage'
import { ProjectSettingsPage } from '../pages/ProjectSettingsPage'
import { SetupPage } from '../pages/SetupPage'
import { AppShell } from './AppShell'
import { NotFound, ProjectRoute } from './ProjectRoute'
import { Root } from './Root'

export const routes: RouteObject[] = [
  {
    element: <Root />,
    children: [
      { path: '/setup', element: <SetupPage /> },
      { path: '/login', element: <LoginPage /> },
      { path: '/invite/:token', element: <InvitePage /> },
      {
        element: (
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        ),
        children: [
          { index: true, element: <Navigate to="/my-issues" replace /> },
          { path: 'my-issues', element: <MyIssuesPage /> },
          {
            path: 'p/:key',
            element: <ProjectRoute />,
            children: [
              { index: true, element: <ProjectIssuesPage /> },
              { path: 'board', element: <ProjectBoardPage /> },
              { path: 'settings', element: <ProjectSettingsPage /> },
            ],
          },
          { path: 'issue/:identifier', element: <IssuePage /> },
          { path: 'settings/members', element: <MembersPage /> },
          { path: 'settings/api-keys', element: <ApiKeysPage /> },
          { path: '*', element: <NotFound what="page" /> },
        ],
      },
    ],
  },
]
