import { useTranslation } from 'react-i18next'

import { useProjects } from '../../api/hooks'
import { compareNames } from '../../i18n'
import { Picker } from '../../ui/Picker'

export type PickerProject = { id: string; key: string; name: string }

type Props = {
  value: string | null
  onSelect: (project: PickerProject) => void
  onClose: () => void
  /** Hide this project (e.g. the issue's current one in "Move to project"). */
  excludeId?: string
  placeholder?: string
}

/** Non-archived projects by name. */
export function ProjectPicker({ value, onSelect, onClose, excludeId, placeholder }: Props) {
  const { t } = useTranslation()
  const projects = useProjects()
  const items = (projects.data ?? []).filter((p) => p.id !== excludeId && !p.archivedAt).sort((a, b) => compareNames(a.name, b.name))
  return (
    <Picker
      items={items}
      value={items.find((p) => p.id === value) ?? null}
      getKey={(p) => p.id}
      getLabel={(p) => `${p.key} ${p.name}`}
      renderItem={(p) => (
        <>
          <span style={{ color: 'var(--fg-muted)', minInlineSize: 40 }}>{p.key}</span>
          <span>{p.name}</span>
        </>
      )}
      onSelect={onSelect}
      onClose={onClose}
      placeholder={placeholder ?? t('pickers.project')}
    />
  )
}
