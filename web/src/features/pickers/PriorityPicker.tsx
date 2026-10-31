import { useTranslation } from 'react-i18next'

import { priorityKey } from '../../ui/priority'
import { PriorityIcon } from '../../ui/PriorityIcon'
import { Picker } from '../../ui/Picker'

const order = [0, 1, 2, 3, 4]

type Props = { value: number; onSelect: (priority: number) => void; onClose: () => void }

/** No priority, Urgent, High, Medium, Low → 0, 1, 2, 3, 4. */
export function PriorityPicker({ value, onSelect, onClose }: Props) {
  const { t } = useTranslation()
  const label = (p: number) => t(`priority.${priorityKey(p)}`)
  return (
    <Picker
      items={order}
      value={value}
      getKey={String}
      getLabel={label}
      renderItem={(p) => (
        <>
          <PriorityIcon priority={p} decorative />
          <span>{label(p)}</span>
        </>
      )}
      onSelect={onSelect}
      onClose={onClose}
      placeholder={t('pickers.priority')}
    />
  )
}
