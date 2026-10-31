import { UserX } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { compareNames } from '../../i18n'
import { Avatar } from '../../ui/Avatar'
import { Picker } from '../../ui/Picker'

export type PickerUser = { id: string; name: string; disabled: boolean }

type Item = { key: string; label: string; userId: string | null }

type Props = {
  users: PickerUser[]
  viewerId: string
  value: string | null
  onSelect: (userId: string | null) => void
  onClose: () => void
}

/** "Me" first, other active users by name, "No assignee" (null) last. */
export function AssigneePicker({ users, viewerId, value, onSelect, onClose }: Props) {
  const { t } = useTranslation()
  const others = users
    .filter((u) => !u.disabled && u.id !== viewerId)
    .sort((a, b) => compareNames(a.name, b.name))
    .map((u): Item => ({ key: u.id, label: u.name, userId: u.id }))
  const items: Item[] = [
    { key: viewerId, label: t('pickers.me'), userId: viewerId },
    ...others,
    { key: 'none', label: t('pickers.noAssignee'), userId: null },
  ]
  return (
    <Picker
      items={items}
      value={items.find((i) => i.userId === value) ?? null}
      getKey={(i) => i.key}
      getLabel={(i) => i.label}
      renderItem={(i) => (
        <>
          {i.userId ? <Avatar id={i.userId} name={i.label} size={18} decorative /> : <UserX size={16} aria-hidden />}
          <span>{i.label}</span>
        </>
      )}
      onSelect={(i) => onSelect(i.userId)}
      onClose={onClose}
      placeholder={t('pickers.assignee')}
    />
  )
}
