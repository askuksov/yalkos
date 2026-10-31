import { Ban, Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Picker } from '../../ui/Picker'
import s from './labels.module.css'
import type { LabelItem } from './useLabels'

type Item = { key: string; label: string; type: LabelItem | null }

type Props = {
  types: LabelItem[]
  value: string | null
  onSelect: (typeId: string | null) => void
  onClose: () => void
  /** Offers "No type" (not in the create dialog: a new issue gets the default type). */
  allowNone?: boolean
}

/** The type group's labels, then "No type". */
export function TypePicker({ types, value, onSelect, onClose, allowNone = true }: Props) {
  const { t } = useTranslation('issues')
  const items: Item[] = [
    ...types.map((ty) => ({ key: ty.id, label: ty.name, type: ty })),
    ...(allowNone ? [{ key: 'none', label: t('labels.noType'), type: null }] : []),
  ]
  return (
    <Picker
      items={items}
      value={items.find((i) => (i.type?.id ?? null) === value) ?? null}
      getKey={(i) => i.key}
      getLabel={(i) => i.label}
      renderItem={(i) => (
        <>
          {i.type ? <span className={s.dot} style={{ background: i.type.color }} aria-hidden /> : <Ban size={14} aria-hidden />}
          <span>{i.label}</span>
          {(i.type?.id ?? null) === value && <Check size={14} className={s.check} aria-hidden />}
        </>
      )}
      onSelect={(i) => onSelect(i.type?.id ?? null)}
      onClose={onClose}
      placeholder={t('labels.type')}
    />
  )
}
