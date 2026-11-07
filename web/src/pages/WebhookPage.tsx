import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate, useParams } from 'react-router'

import { errorMessage, GraphQLRequestError, request } from '../api/client'
import {
  DeleteWebhookMutation,
  RotateWebhookSecretMutation,
  TestWebhookMutation,
  UpdateWebhookMutation,
  WebhookQuery,
} from '../api/operations'
import { NotFound } from '../app/ProjectRoute'
import { PageHeader } from '../app/PageHeader'
import { DeliveriesTable } from '../features/webhooks/DeliveriesTable'
import { WebhookForm } from '../features/webhooks/WebhookForm'
import { displayName, errorField, webhookErrorMessage, type WebhookFormValues } from '../features/webhooks/webhooks'
import w from '../features/webhooks/webhooks.module.css'
import type { UpdateWebhookInput, WebhookQuery as WebhookResult } from '../gql/graphql'
import { formatDateTime } from '../lib/date'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { useToast } from '../ui/useToast'

type Hook = WebhookResult['webhook']

/** One webhook: status, settings, signing secret, test, delete and the delivery log. */
export function WebhookPage() {
  const { t } = useTranslation(['webhooks', 'common'])
  const { id = '' } = useParams()
  const hook = useQuery({ queryKey: ['webhook', id], queryFn: () => request(WebhookQuery, { id }), select: (d) => d.webhook })

  if (hook.error instanceof GraphQLRequestError && hook.error.code === 'NOT_FOUND') return <NotFound what="page" />
  if (hook.error) return <p role="alert" className={w.content}>{t('common:status.loadFailed')}</p>
  if (!hook.data) return <p className={w.content}>{t('common:status.loading')}</p>
  return <Loaded hook={hook.data} refreshing={hook.isFetching} onRefresh={() => void hook.refetch()} />
}

function Loaded({ hook, refreshing, onRefresh }: { hook: Hook; refreshing: boolean; onRefresh: () => void }) {
  const { t } = useTranslation(['webhooks', 'common'])
  const qc = useQueryClient()
  const toast = useToast()
  const [errors, setErrors] = useState<Record<string, string>>({})
  const refresh = () => Promise.all([qc.invalidateQueries({ queryKey: ['webhook', hook.id] }), qc.invalidateQueries({ queryKey: ['webhooks'] })])
  const update = useMutation({
    mutationFn: (input: UpdateWebhookInput) => request(UpdateWebhookMutation, { id: hook.id, input }),
    onSuccess: () => {
      setErrors({})
      toast.show(t('saved'))
      void refresh()
    },
    onError: (err) => setErrors({ [errorField(err)]: webhookErrorMessage(err) }),
  })

  const initial: WebhookFormValues = {
    url: hook.url,
    name: hook.name,
    projectId: hook.project?.id ?? '',
    resources: hook.resources,
    secret: '',
    enabled: hook.enabled,
  }
  // The form restarts from the server values whenever they change (after a save or an enable).
  const formKey = JSON.stringify([initial.url, initial.name, initial.projectId, initial.resources, initial.enabled])
  const save = (v: WebhookFormValues) => {
    const input: UpdateWebhookInput = {}
    if (v.url.trim() !== initial.url) input.url = v.url.trim()
    if (v.name.trim() !== initial.name) input.name = v.name.trim()
    if (v.projectId !== initial.projectId) input.projectId = v.projectId || null
    if (v.resources.join() !== initial.resources.join()) input.resources = v.resources
    if (v.enabled !== initial.enabled) input.enabled = v.enabled
    setErrors({})
    update.mutate(input)
  }

  return (
    <>
      <PageHeader
        title={displayName(hook)}
        prefix={
          <Link to="/settings/webhooks" aria-label={t('title')} title={t('title')}>
            <ChevronLeft size={16} aria-hidden />
          </Link>
        }
      />
      <div className={w.content}>
        {hook.disabledReason === 'FAILURES' && (
          <section className={w.banner}>
            <p>{hook.disabledAt ? t('banner.failures', { date: formatDateTime(hook.disabledAt) }) : t('banner.failuresNoDate')}</p>
            <Button variant="primary" disabled={update.isPending} onClick={() => update.mutate({ enabled: true })}>
              {t('banner.enable')}
            </Button>
          </section>
        )}
        <WebhookForm
          key={formKey}
          mode="edit"
          initial={initial}
          currentProject={hook.project}
          pending={update.isPending}
          errors={errors}
          onSubmit={save}
        />
        <SecretSection hook={hook} />
        <TestSection hook={hook} />
        <section className={w.section} aria-labelledby="deliveries-title">
          <div className={w.sectionHead}>
            <h2 id="deliveries-title">{t('deliveries.title')}</h2>
            <Button size="sm" disabled={refreshing} onClick={onRefresh}>
              {t('deliveries.refresh')}
            </Button>
          </div>
          <DeliveriesTable deliveries={hook.deliveries} />
        </section>
        <DeleteSection hook={hook} />
      </div>
    </>
  )
}

function SecretSection({ hook }: { hook: Hook }) {
  const { t } = useTranslation(['webhooks', 'common'])
  const qc = useQueryClient()
  const toast = useToast()
  // Right after creation the secret is shown once.
  const [shown, setShown] = useState(!!(useLocation().state as { revealSecret?: boolean } | null)?.revealSecret)
  const [rotating, setRotating] = useState(false)
  const rotate = useMutation({
    mutationFn: () => request(RotateWebhookSecretMutation, { id: hook.id }),
    onSuccess: (d) => {
      qc.setQueryData(['webhook', hook.id], (old: { webhook: Hook } | undefined) =>
        old && { webhook: { ...old.webhook, secret: d.rotateWebhookSecret.secret } },
      )
      void qc.invalidateQueries({ queryKey: ['webhook', hook.id] })
      setRotating(false)
      setShown(true)
      toast.show(t('secret.rotated'))
    },
    onError: (err) => {
      setRotating(false)
      toast.show(errorMessage(err), { kind: 'error' })
    },
  })
  const masked = hook.secret.startsWith('yal_wh_') ? t('secret.masked') : t('secret.maskedCustom')
  return (
    <section className={w.section} aria-labelledby="secret-title">
      <h2 id="secret-title">{t('secret.title')}</h2>
      <div className={w.secretRow}>
        <code>{shown ? hook.secret : masked}</code>
        <Button size="sm" onClick={() => setShown(!shown)}>
          {shown ? t('secret.hide') : t('secret.show')}
        </Button>
        <Button
          size="sm"
          onClick={() => {
            void navigator.clipboard?.writeText(hook.secret)
            toast.show(t('common:status.copied'))
          }}
        >
          {t('secret.copy')}
        </Button>
        <Button size="sm" onClick={() => setRotating(true)}>
          {t('secret.rotate')}
        </Button>
      </div>
      {shown && <p className={w.hint}>{t('secret.copyHint')}</p>}
      <Dialog open={rotating} onClose={() => setRotating(false)} title={t('secret.rotateTitle')}>
        <p>{t('secret.rotateBody')}</p>
        <div className={w.actions}>
          <Button onClick={() => setRotating(false)}>{t('common:actions.cancel')}</Button>
          <Button variant="primary" disabled={rotate.isPending} onClick={() => rotate.mutate()}>
            {t('secret.rotateConfirm')}
          </Button>
        </div>
      </Dialog>
    </section>
  )
}

function TestSection({ hook }: { hook: Hook }) {
  const { t } = useTranslation('webhooks')
  const qc = useQueryClient()
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  const test = useMutation({
    mutationFn: () => request(TestWebhookMutation, { id: hook.id }),
    onMutate: () => setResult(null),
    onSuccess: ({ testWebhook: d }) => {
      setResult(
        d.status === 'SUCCEEDED'
          ? { ok: true, text: t('test.delivered', { status: d.responseStatus ?? t('deliveries.none'), ms: d.durationMs ?? 0 }) }
          : { ok: false, text: d.error ? t('test.failed', { error: d.error }) : t('test.failedNoReason') },
      )
      void qc.invalidateQueries({ queryKey: ['webhook', hook.id] })
    },
    onError: (err) => setResult({ ok: false, text: errorMessage(err) }),
  })
  return (
    <section className={w.section}>
      <div className={w.secretRow}>
        <Button disabled={test.isPending} onClick={() => test.mutate()}>
          {t('test.send')}
        </Button>
        {result && (
          <p role="status" className={w.result} data-ok={result.ok}>
            {result.text}
          </p>
        )}
      </div>
    </section>
  )
}

function DeleteSection({ hook }: { hook: Hook }) {
  const { t } = useTranslation(['webhooks', 'common'])
  const qc = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const [confirming, setConfirming] = useState(false)
  const remove = useMutation({
    mutationFn: () => request(DeleteWebhookMutation, { id: hook.id }),
    onSuccess: () => {
      toast.show(t('delete.deleted'))
      void navigate('/settings/webhooks', { replace: true })
      qc.removeQueries({ queryKey: ['webhook', hook.id] })
      void qc.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError: (err) => {
      setConfirming(false)
      toast.show(errorMessage(err), { kind: 'error' })
    },
  })
  return (
    <section className={[w.section, w.danger].join(' ')}>
      <div>
        <Button variant="danger" onClick={() => setConfirming(true)}>
          {t('delete.button')}
        </Button>
      </div>
      <Dialog open={confirming} onClose={() => setConfirming(false)} title={t('delete.title')}>
        <p>{t('delete.body')}</p>
        <div className={w.actions}>
          <Button onClick={() => setConfirming(false)}>{t('common:actions.cancel')}</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('delete.confirm')}
          </Button>
        </div>
      </Dialog>
    </section>
  )
}
