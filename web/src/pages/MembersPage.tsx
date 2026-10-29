import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { Link } from 'react-router'

import { errorMessage, request } from '../api/client'
import {
  CreateInviteMutation, InvitesQuery, MembersQuery, RevokeInviteMutation, SetMemberDisabledMutation,
  UpdateMemberRoleMutation, ViewerQuery,
} from '../api/operations'
import type { Role } from '../gql/graphql'
import { formatDateTime } from '../lib/date'
import { Layout } from './Layout'

export function MembersPage() {
  const qc = useQueryClient()
  const viewer = useQuery({ queryKey: ['viewer'], queryFn: () => request(ViewerQuery) })
  const me = viewer.data?.viewer
  const isOwner = me?.role === 'OWNER'
  const isAdmin = isOwner || me?.role === 'ADMIN'
  const members = useQuery({ queryKey: ['members'], queryFn: () => request(MembersQuery) })
  const invites = useQuery({ queryKey: ['invites'], queryFn: () => request(InvitesQuery), enabled: isAdmin })
  const [role, setRole] = useState<Role>('MEMBER')
  const [inviteURL, setInviteURL] = useState('')

  const refresh = () => qc.invalidateQueries({ queryKey: ['members'] }).then(() => qc.invalidateQueries({ queryKey: ['invites'] }))
  const createInvite = useMutation({
    mutationFn: () => request(CreateInviteMutation, { role, days: 7 }),
    onSuccess: (d) => {
      setInviteURL(d.createInvite.url)
      void refresh()
    },
  })
  const revokeInvite = useMutation({ mutationFn: (id: string) => request(RevokeInviteMutation, { id }), onSuccess: refresh })
  const changeRole = useMutation({
    mutationFn: (v: { userId: string; role: Role }) => request(UpdateMemberRoleMutation, v),
    onSuccess: refresh,
  })
  const toggle = useMutation({
    mutationFn: (v: { userId: string; disabled: boolean }) => request(SetMemberDisabledMutation, v),
    onSuccess: refresh,
  })
  const error = createInvite.error ?? revokeInvite.error ?? changeRole.error ?? toggle.error

  const submit = (e: FormEvent) => {
    e.preventDefault()
    createInvite.mutate()
  }

  return (
    <Layout title="Members">
      <Link to="/">← Home</Link>
      {error && <p role="alert">{errorMessage(error)}</p>}
      <table>
        <thead><tr><th>Name</th><th>Email</th><th>Role</th><th>Status</th></tr></thead>
        <tbody>
          {members.data?.members.map((m) => (
            <tr key={m.id}>
              <td>{m.name}</td>
              <td>{m.email}</td>
              <td>
                {isOwner && m.role !== 'OWNER' ? (
                  <select aria-label={`Role of ${m.name}`} value={m.role}
                    onChange={(e) => changeRole.mutate({ userId: m.id, role: e.target.value as Role })}>
                    <option value="ADMIN">admin</option>
                    <option value="MEMBER">member</option>
                  </select>
                ) : m.role.toLowerCase()}
              </td>
              <td>
                {isAdmin && m.role !== 'OWNER' && m.id !== me?.id ? (
                  <button type="button" onClick={() => toggle.mutate({ userId: m.id, disabled: !m.disabled })}>
                    {m.disabled ? 'Enable' : 'Disable'}
                  </button>
                ) : m.disabled ? 'disabled' : 'active'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {isAdmin && (
        <section>
          <h2>Invite</h2>
          <form onSubmit={submit}>
            <label>
              Role
              <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
                <option value="MEMBER">member</option>
                {isOwner && <option value="ADMIN">admin</option>}
              </select>
            </label>
            <button type="submit" disabled={createInvite.isPending}>Create invite link</button>
          </form>
          {inviteURL && (
            <p>
              Send this link (valid 7 days, single use): <code>{inviteURL}</code>{' '}
              <button type="button" onClick={() => void navigator.clipboard?.writeText(inviteURL)}>Copy</button>
            </p>
          )}
          <ul>
            {invites.data?.invites.map((i) => (
              <li key={i.id}>
                {i.role.toLowerCase()} · expires {formatDateTime(i.expiresAt)}{' '}
                <button type="button" onClick={() => revokeInvite.mutate(i.id)}>Revoke</button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </Layout>
  )
}
