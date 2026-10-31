import { useTranslation } from 'react-i18next'

import s from './labels.module.css'
import type { IssueLabel } from './useLabels'

const maxShown = 3

/** The type chip, then up to three other labels and "+n" for the rest. */
export function LabelChips({ type, labels }: { type: IssueLabel | null; labels: IssueLabel[] }) {
  const { t } = useTranslation('issues')
  const others = labels.filter((l) => l.id !== type?.id)
  const shown = others.slice(0, maxShown)
  const rest = others.length - shown.length
  if (!type && shown.length === 0) return null
  return (
    <span className={s.chips}>
      {type && <Chip label={type} strong />}
      {shown.map((l) => (
        <Chip key={l.id} label={l} />
      ))}
      {rest > 0 && (
        <span className={s.more} title={t('labels.more', { names: others.slice(maxShown).map((l) => l.name).join(', ') })}>
          +{rest}
        </span>
      )}
    </span>
  )
}

export function Chip({ label, strong }: { label: IssueLabel; strong?: boolean }) {
  return (
    <span className={[s.chip, strong ? s.typeChip : ''].join(' ')} data-testid="label-chip">
      <span className={s.dot} style={{ background: label.color }} aria-hidden />
      {label.name}
    </span>
  )
}
