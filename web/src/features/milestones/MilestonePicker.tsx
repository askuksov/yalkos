import { Ban, Target } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Picker } from '../../ui/Picker'

type Item = { key: string; label: string; id: string | null }

type Props = {
  milestones: { id: string; name: string }[]
  value: string | null
  onSelect: (milestoneId: string | null) => void
  onClose: () => void
}

/** The project's milestones in manual order, then "No milestone". */
export function MilestonePicker({ milestones, value, onSelect, onClose }: Props) {
  const { t } = useTranslation('issues')
  const items: Item[] = [...milestones.map((m) => ({ key: m.id, label: m.name, id: m.id })), { key: 'none', label: t('milestones.none'), id: null }]
  return (
    <Picker
      items={items}
      value={items.find((i) => i.id === value) ?? null}
      getKey={(i) => i.key}
      getLabel={(i) => i.label}
      renderItem={(i) => (
        <>
          {i.id ? <Target size={14} aria-hidden /> : <Ban size={14} aria-hidden />}
          <span>{i.label}</span>
        </>
      )}
      onSelect={(i) => onSelect(i.id)}
      onClose={onClose}
      placeholder={t('milestones.milestone')}
    />
  )
}
