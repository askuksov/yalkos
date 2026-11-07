import { ChevronDown, ChevronRight } from 'lucide-react'
import { Fragment, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { WebhookDeliveryFieldsFragment } from '../../gql/graphql'
import { formatDateTime } from '../../lib/date'
import { eventLabel, maxAttempts } from './webhooks'
import s from './webhooks.module.css'

/** The delivery log, newest first; a row expands to the error text and the delivery id. */
export function DeliveriesTable({ deliveries }: { deliveries: WebhookDeliveryFieldsFragment[] }) {
  const { t } = useTranslation('webhooks')
  const [open, setOpen] = useState<Set<string>>(new Set())
  const toggle = (id: string) =>
    setOpen((cur) => {
      const next = new Set(cur)
      if (!next.delete(id)) next.add(id)
      return next
    })
  if (deliveries.length === 0) return <p className={s.muted}>{t('deliveries.empty')}</p>
  return (
    <table className={s.table} aria-label={t('deliveries.title')}>
      <thead>
        <tr>
          <th />
          <th>{t('deliveries.time')}</th>
          <th>{t('deliveries.event')}</th>
          <th>{t('deliveries.status')}</th>
          <th>{t('deliveries.response')}</th>
          <th>{t('deliveries.attempts')}</th>
          <th>{t('deliveries.duration')}</th>
          <th>{t('deliveries.nextAttempt')}</th>
        </tr>
      </thead>
      <tbody>
        {deliveries.map((d) => {
          const expanded = open.has(d.id)
          return (
            <Fragment key={d.id}>
              <tr>
                <td>
                  <button
                    type="button"
                    className={s.toggleBtn}
                    aria-expanded={expanded}
                    aria-label={expanded ? t('deliveries.hideDetails') : t('deliveries.showDetails')}
                    onClick={() => toggle(d.id)}
                  >
                    {expanded ? <ChevronDown size={14} aria-hidden /> : <ChevronRight size={14} aria-hidden />}
                  </button>
                </td>
                <td className={s.num}>{formatDateTime(d.createdAt)}</td>
                <td>{eventLabel(d.resource, d.action)}</td>
                <td>
                  <span className={s.chip} data-status={d.status}>
                    {t(`status.${d.status}`)}
                  </span>
                </td>
                <td className={s.num}>{d.responseStatus ?? t('deliveries.none')}</td>
                <td className={s.num}>{t('deliveries.attemptsOf', { attempts: d.attempts, max: maxAttempts })}</td>
                <td className={s.num}>{d.durationMs == null ? t('deliveries.none') : t('deliveries.ms', { ms: d.durationMs })}</td>
                <td className={s.num}>{d.status === 'RETRYING' && d.nextAttemptAt ? formatDateTime(d.nextAttemptAt) : t('deliveries.none')}</td>
              </tr>
              {expanded && (
                <tr className={s.details}>
                  <td colSpan={8}>
                    <dl>
                      <dt>{t('deliveries.error')}</dt>
                      <dd>{d.error ?? t('deliveries.noError')}</dd>
                      <dt>{t('deliveries.id')}</dt>
                      <dd>
                        <code>{d.id}</code>
                      </dd>
                    </dl>
                  </td>
                </tr>
              )}
            </Fragment>
          )
        })}
      </tbody>
    </table>
  )
}
