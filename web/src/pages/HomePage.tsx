import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'

import { request } from '../api/client'
import { LogoutMutation, ViewerQuery } from '../api/operations'
import { Layout } from './Layout'

export function HomePage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const viewer = useQuery({ queryKey: ['viewer'], queryFn: () => request(ViewerQuery) })
  const logout = useMutation({
    mutationFn: () => request(LogoutMutation),
    onSuccess: () => {
      qc.clear()
      navigate('/login', { replace: true })
    },
  })
  const v = viewer.data?.viewer
  return (
    <Layout title="Home">
      {v && <p>Signed in as {v.name} ({v.role.toLowerCase()})</p>}
      <nav>
        <Link to="/settings/members">Members</Link> · <Link to="/settings/api-keys">API keys</Link>
      </nav>
      <button type="button" onClick={() => logout.mutate()}>Log out</button>
    </Layout>
  )
}
