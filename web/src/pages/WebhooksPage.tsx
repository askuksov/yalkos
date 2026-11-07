import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router'

import { request } from '../api/client'
import { CreateWebhookMutation, WebhooksQuery } from '../api/operations'
import { PageHeader } from '../app/PageHeader'
import type { WebhooksQuery as WebhooksResult } from '../gql/graphql'
import { formatDateTime } from '../lib/date'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { WebhookForm } from '../features/webhooks/WebhookForm'
import { displayName, emptyWebhookForm, errorField, resourceOrder, webhookErrorMessage, type WebhookFormValues } from '../features/webhooks/webhooks'
import w from '../features/webhooks/webhooks.module.css'

type Row = WebhooksResult['webhooks'][number]

/** Settings → Webhooks: every webhook of the workspace and "New webhook". */
export function WebhooksPage() {
  const { t } = useTranslation(['webhooks', 'common'])
  const list = useQuery({ queryKey: ['webhooks'], queryFn: () => request(WebhooksQuery), select: (d) => d.webhooks })
  const [creating, setCreating] = useState(false)
  const rows = list.data ?? []
  return (
    <>
      <PageHeader title={t('title')}>
        <Button size="sm" variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setCreating(true)}>
          {t('new')}
        </Button>
      </PageHeader>
      <div className={w.content}>
        <p className={w.muted}>{t('description')}</p>
        {list.error && <p role="alert">{t('common:status.loadFailed')}</p>}
        {list.data && rows.length === 0 && <p>{t('empty')}</p>}
        {rows.length > 0 && (
          <table className={w.table}>
            <thead>
              <tr>
                <th>{t('list.name')}</th>
                <th>{t('list.scope')}</th>
                <th>{t('list.events')}</th>
                <th>{t('list.status')}</th>
                <th>{t('list.lastDelivery')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <WebhookRow key={row.id} row={row} />
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Dialog open={creating} onClose={() => setCreating(false)} title={t('new')} wide>
        {creating && <NewWebhook onClose={() => setCreating(false)} />}
      </Dialog>
    </>
  )
}

function WebhookRow({ row }: { row: Row }) {
  const { t } = useTranslation('webhooks')
  const navigate = useNavigate()
  const path = `/settings/webhooks/${row.id}`
  const last = row.deliveries[0]
  const state = !row.enabled ? (row.disabledReason === 'FAILURES' ? 'failures' : 'disabled') : 'enabled'
  const events = resourceOrder.filter((r) => row.resources.includes(r)).map((r) => t(`resource.${r}`))
  return (
    <tr
      className={w.clickable}
      onClick={(e) => {
        // A click on the link itself already navigates.
        if (!(e.target as HTMLElement).closest('a')) void navigate(path)
      }}
    >
      <td>
        <div className={w.nameCell}>
          <Link to={path}>{displayName(row)}</Link>
          <span className={w.url} title={row.url}>
            {row.url}
          </span>
        </div>
      </td>
      <td>{row.project ? row.project.key : t('scope.all')}</td>
      <td>{events.join(t('listSeparator'))}</td>
      <td>
        <span className={w.badge} data-state={state}>
          {t(`state.${state}`)}
        </span>
      </td>
      <td>
        {last ? (
          <>
            <span className={w.chip} data-status={last.status}>
              {t(`status.${last.status}`)}
            </span>{' '}
            <span className={w.num}>{formatDateTime(last.lastAttemptAt ?? last.createdAt)}</span>
          </>
        ) : (
          t('list.none')
        )}
      </td>
    </tr>
  )
}

function NewWebhook({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [errors, setErrors] = useState<Record<string, string>>({})
  const create = useMutation({
    mutationFn: (v: WebhookFormValues) =>
      request(CreateWebhookMutation, {
        input: {
          url: v.url.trim(),
          resources: v.resources,
          enabled: v.enabled,
          ...(v.name.trim() ? { name: v.name.trim() } : {}),
          ...(v.projectId ? { projectId: v.projectId } : {}),
          // The secret is used as typed.
          ...(v.secret ? { secret: v.secret } : {}),
        },
      }),
    onSuccess: (d) => {
      void qc.invalidateQueries({ queryKey: ['webhooks'] })
      onClose()
      void navigate(`/settings/webhooks/${d.createWebhook.id}`, { state: { revealSecret: true } })
    },
    onError: (err) => setErrors({ [errorField(err)]: webhookErrorMessage(err) }),
  })
  return (
    <WebhookForm
      mode="create"
      initial={emptyWebhookForm}
      pending={create.isPending}
      errors={errors}
      onSubmit={(v) => {
        setErrors({})
        create.mutate(v)
      }}
      onCancel={onClose}
    />
  )
}
