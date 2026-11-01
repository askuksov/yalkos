import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { Trans, useTranslation } from 'react-i18next'

import s from '../app/app.module.css'
import { PageHeader } from '../app/PageHeader'
import { errorMessage, request } from '../api/client'
import { ApiKeysQuery, CreateApiKeyMutation, RevokeApiKeyMutation } from '../api/operations'
import { formatDateTime } from '../lib/date'
import { Button } from '../ui/Button'
import { AgentSetup } from './AgentSetup'

export function ApiKeysPage() {
  const { t } = useTranslation(['settings', 'common'])
  const qc = useQueryClient()
  const keys = useQuery({ queryKey: ['apiKeys'], queryFn: () => request(ApiKeysQuery) })
  const [name, setName] = useState('')
  const [created, setCreated] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['apiKeys'] })
  const create = useMutation({
    mutationFn: () => request(CreateApiKeyMutation, { name }),
    onSuccess: (d) => {
      setCreated(d.createApiKey.key)
      setName('')
      void refresh()
    },
  })
  const revoke = useMutation({ mutationFn: (id: string) => request(RevokeApiKeyMutation, { id }), onSuccess: refresh })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate()
  }

  return (
    <>
      <PageHeader title={t('apiKeys.title')} />
      <div className={s.content}>
        <p>
          <Trans t={t} i18nKey="apiKeys.hint" values={{ header: t('apiKeys.headerExample') }} components={{ code: <code /> }} />
        </p>
        {created ? (
          <section className="secret">
            <p>
              <strong>{t('apiKeys.once')}</strong> {t('apiKeys.copyNow')}
            </p>
            <code>{created}</code>{' '}
            <Button size="sm" onClick={() => void navigator.clipboard?.writeText(created)}>
              {t('common:actions.copy')}
            </Button>{' '}
            <Button size="sm" onClick={() => setCreated('')}>
              {t('common:actions.done')}
            </Button>
          </section>
        ) : (
          <form onSubmit={submit}>
            <label>
              {t('apiKeys.name')}
              <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={100} />
            </label>
            <Button type="submit" variant="primary" disabled={create.isPending}>
              {t('apiKeys.create')}
            </Button>
          </form>
        )}
        {(create.error ?? revoke.error) && <p role="alert">{errorMessage(create.error ?? revoke.error)}</p>}
        <table>
          <thead>
            <tr>
              <th>{t('members.name')}</th>
              <th>{t('apiKeys.key')}</th>
              <th>{t('apiKeys.created')}</th>
              <th>{t('apiKeys.lastUsed')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {keys.data?.apiKeys.map((k) => (
              <tr key={k.id}>
                <td>{k.name}</td>
                <td>
                  <code>{k.displayPrefix}</code>
                </td>
                <td>{formatDateTime(k.createdAt)}</td>
                <td>{k.lastUsedAt ? formatDateTime(k.lastUsedAt) : t('apiKeys.never')}</td>
                <td>
                  <Button size="sm" onClick={() => revoke.mutate(k.id)}>
                    {t('apiKeys.revoke')}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <AgentSetup apiKey={created} />
      </div>
    </>
  )
}
