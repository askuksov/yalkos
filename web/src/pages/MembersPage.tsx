import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'

import s from '../app/app.module.css'
import { PageHeader } from '../app/PageHeader'
import { errorMessage, request } from '../api/client'
import { useViewer } from '../api/hooks'
import {
  CreateInviteMutation, InvitesQuery, MembersQuery, RevokeInviteMutation, SetMemberDisabledMutation,
  UpdateMemberRoleMutation,
} from '../api/operations'
import type { Role } from '../gql/graphql'
import { formatDateTime } from '../lib/date'
import { Button } from '../ui/Button'

export function MembersPage() {
  const { t } = useTranslation(['settings', 'common'])
  const qc = useQueryClient()
  const me = useViewer().data
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
    <>
      <PageHeader title={t('members.title')} />
      <div className={s.content}>
        {error && <p role="alert">{errorMessage(error)}</p>}
        <table>
          <thead>
            <tr>
              <th>{t('members.name')}</th>
              <th>{t('members.email')}</th>
              <th>{t('members.role')}</th>
              <th>{t('members.status')}</th>
            </tr>
          </thead>
          <tbody>
            {members.data?.members.map((m) => (
              <tr key={m.id}>
                <td>{m.name}</td>
                <td>{m.email}</td>
                <td>
                  {isOwner && m.role !== 'OWNER' ? (
                    <select
                      aria-label={t('members.roleOf', { name: m.name })}
                      value={m.role}
                      onChange={(e) => changeRole.mutate({ userId: m.id, role: e.target.value as Role })}
                    >
                      <option value="ADMIN">{t('roles.ADMIN')}</option>
                      <option value="MEMBER">{t('roles.MEMBER')}</option>
                    </select>
                  ) : (
                    t(`roles.${m.role}`)
                  )}
                </td>
                <td>
                  {isAdmin && m.role !== 'OWNER' && m.id !== me?.id ? (
                    <Button size="sm" onClick={() => toggle.mutate({ userId: m.id, disabled: !m.disabled })}>
                      {m.disabled ? t('members.enable') : t('members.disable')}
                    </Button>
                  ) : m.disabled ? (
                    t('members.disabled')
                  ) : (
                    t('members.active')
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {isAdmin && (
          <section>
            <h2>{t('members.invite')}</h2>
            <form onSubmit={submit}>
              <label>
                {t('members.inviteRole')}
                <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
                  <option value="MEMBER">{t('roles.MEMBER')}</option>
                  {isOwner && <option value="ADMIN">{t('roles.ADMIN')}</option>}
                </select>
              </label>
              <Button type="submit" variant="primary" disabled={createInvite.isPending}>
                {t('members.createInvite')}
              </Button>
            </form>
            {inviteURL && (
              <p>
                {t('members.inviteLink')} <code>{inviteURL}</code>{' '}
                <Button size="sm" onClick={() => void navigator.clipboard?.writeText(inviteURL)}>
                  {t('common:actions.copy')}
                </Button>
              </p>
            )}
            <ul>
              {invites.data?.invites.map((i) => (
                <li key={i.id}>
                  {t('members.inviteExpires', { role: t(`roles.${i.role}`), date: formatDateTime(i.expiresAt) })}{' '}
                  <Button size="sm" onClick={() => revokeInvite.mutate(i.id)}>
                    {t('members.revoke')}
                  </Button>
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </>
  )
}
