import { useTranslation } from 'react-i18next'

import type { MilestoneStatus as Status } from '../../gql/graphql'
import s from './milestones.module.css'
import { statusKey } from './status'

const colors: Record<Status, string> = {
  PLANNED: 'var(--fg-muted)',
  IN_PROGRESS: '#f2c94c',
  DONE: '#5e6ad2',
  CANCELED: '#95a2b3',
}

/** A status dot with its name. */
export function MilestoneStatus({ status }: { status: Status }) {
  const { t } = useTranslation('issues')
  return (
    <span className={s.status}>
      <span className={s.statusDot} style={{ background: colors[status] }} aria-hidden />
      {t(`milestones.status.${statusKey(status)}`)}
    </span>
  )
}
