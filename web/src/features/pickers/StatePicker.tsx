import { useTranslation } from 'react-i18next'

import { Picker } from '../../ui/Picker'
import { StateIcon, type StateTypeName } from '../../ui/StateIcon'

export type PickerState = { id: string; name: string; type: StateTypeName; color: string; position: number }

type Props = { states: PickerState[]; value: string | null; onSelect: (stateId: string) => void; onClose: () => void }

/** States of one project, by position. */
export function StatePicker({ states, value, onSelect, onClose }: Props) {
  const { t } = useTranslation()
  const items = [...states].sort((a, b) => a.position - b.position)
  return (
    <Picker
      items={items}
      value={items.find((s) => s.id === value) ?? null}
      getKey={(s) => s.id}
      getLabel={(s) => s.name}
      renderItem={(s) => (
        <>
          <StateIcon type={s.type} color={s.color} />
          <span>{s.name}</span>
        </>
      )}
      onSelect={(s) => onSelect(s.id)}
      onClose={onClose}
      placeholder={t('pickers.state')}
    />
  )
}
