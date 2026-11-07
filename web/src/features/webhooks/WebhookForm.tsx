import { type FormEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useProjects } from '../../api/hooks'
import type { WebhookResource } from '../../gql/graphql'
import { compareNames } from '../../i18n'
import { Button } from '../../ui/Button'
import { resourceOrder, type WebhookFormValues } from './webhooks'
import s from './webhooks.module.css'

type Props = {
  mode: 'create' | 'edit'
  initial: WebhookFormValues
  /** The webhook's current project, kept in the select even when it is archived. */
  currentProject?: { id: string; name: string } | null
  pending: boolean
  /** Messages by field (url, name, projectId, resources, secret) and "form". */
  errors: Record<string, string>
  onSubmit: (values: WebhookFormValues) => void
  onCancel?: () => void
}

function sameResources(a: WebhookResource[], b: WebhookResource[]): boolean {
  return a.length === b.length && a.every((r) => b.includes(r))
}

/** URL, name, project, event types and enabled flag; the secret field only when creating. */
export function WebhookForm({ mode, initial, currentProject, pending, errors, onSubmit, onCancel }: Props) {
  const { t } = useTranslation(['webhooks', 'common'])
  const projects = useProjects().data ?? []
  const ids = { url: useId(), name: useId(), project: useId(), secret: useId(), enabled: useId() }
  const [values, setValues] = useState(initial)
  const set = <K extends keyof WebhookFormValues>(k: K, v: WebhookFormValues[K]) => setValues({ ...values, [k]: v })
  const toggle = (r: WebhookResource) =>
    set('resources', resourceOrder.filter((x) => (x === r ? !values.resources.includes(r) : values.resources.includes(x))))

  const options: { id: string; name: string }[] = [...projects].sort((a, b) => compareNames(a.name, b.name))
  if (currentProject && !options.some((p) => p.id === currentProject.id)) options.push(currentProject)

  const dirty =
    values.url.trim() !== initial.url ||
    values.name.trim() !== initial.name ||
    values.projectId !== initial.projectId ||
    values.enabled !== initial.enabled ||
    !sameResources(values.resources, initial.resources)
  const canSubmit = !!values.url.trim() && !pending && (mode === 'create' || dirty)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (canSubmit) onSubmit(values)
  }
  const err = (k: string) =>
    errors[k] && (
      <p role="alert" className={s.error}>
        {errors[k]}
      </p>
    )

  return (
    <form onSubmit={submit} className={s.section}>
      <div className={s.form}>
        <label htmlFor={ids.url}>{t('form.url')}</label>
        <div className={s.field}>
          <input
            id={ids.url}
            inputMode="url"
            autoComplete="off"
            required
            placeholder="https://"
            value={values.url}
            onChange={(e) => set('url', e.target.value)}
            aria-invalid={!!errors.url || undefined}
          />
          {err('url')}
        </div>
        <label htmlFor={ids.name}>{t('form.name')}</label>
        <div className={s.field}>
          <input id={ids.name} maxLength={100} value={values.name} onChange={(e) => set('name', e.target.value)} />
          {err('name')}
        </div>
        <label htmlFor={ids.project}>{t('form.project')}</label>
        <div className={s.field}>
          <select id={ids.project} value={values.projectId} onChange={(e) => set('projectId', e.target.value)}>
            <option value="">{t('scope.all')}</option>
            {options.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          {err('projectId')}
        </div>
        <span id={`${ids.project}-events`}>{t('form.events')}</span>
        <div className={s.field}>
          <div role="group" aria-labelledby={`${ids.project}-events`} className={s.checks}>
            {resourceOrder.map((r) => (
              <label key={r} className={s.check}>
                <input type="checkbox" checked={values.resources.includes(r)} onChange={() => toggle(r)} />
                {t(`resource.${r}`)}
              </label>
            ))}
          </div>
          {err('resources')}
        </div>
        {mode === 'create' && (
          <>
            <label htmlFor={ids.secret}>{t('form.secret')}</label>
            <div className={s.field}>
              <input id={ids.secret} autoComplete="off" maxLength={255} value={values.secret} onChange={(e) => set('secret', e.target.value)} />
              <p className={s.hint}>{t('form.secretHint')}</p>
              {err('secret')}
            </div>
          </>
        )}
        <label htmlFor={ids.enabled}>{t('form.enabled')}</label>
        <div className={s.field}>
          <input id={ids.enabled} type="checkbox" checked={values.enabled} onChange={(e) => set('enabled', e.target.checked)} className={s.toggle} />
        </div>
      </div>
      {err('form')}
      {mode === 'create' ? (
        <div className={s.actions}>
          <Button onClick={onCancel}>{t('common:actions.cancel')}</Button>
          <Button type="submit" variant="primary" disabled={!canSubmit}>
            {t('form.create')}
          </Button>
        </div>
      ) : (
        dirty && (
          <div className={s.actions}>
            <Button type="submit" variant="primary" disabled={!canSubmit}>
              {t('form.save')}
            </Button>
          </div>
        )
      )}
    </form>
  )
}
