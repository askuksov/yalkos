import { useTranslation } from 'react-i18next'

import s from './milestones.module.css'

/** Bar and "completed/total" of a milestone's issues. */
export function MilestoneProgress({ completed, total }: { completed: number; total: number }) {
  const { t } = useTranslation('issues')
  const pct = total ? Math.round((completed / total) * 100) : 0
  return (
    <span className={s.progress}>
      <span
        className={s.bar}
        role="progressbar"
        aria-label={t('milestones.progress')}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={completed}
      >
        <span className={s.fill} style={{ inlineSize: `${pct}%` }} />
      </span>
      <span className={s.count}>
        {completed}/{total}
      </span>
    </span>
  )
}
