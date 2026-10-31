import { Check, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Picker } from '../../ui/Picker'
import s from './labels.module.css'
import type { LabelCatalog, LabelItem } from './useLabels'

type Item = { key: string; label: string; group?: string; target?: LabelItem; create?: string }

type Props = {
  labels: LabelCatalog
  /** The issue's label ids. */
  selected: string[]
  onToggle: (label: LabelItem, on: boolean) => void
  /** Creates a free label named so and applies it. */
  onCreate: (name: string) => void
  onClose: () => void
}

/**
 * Multi-select over free labels and labels of groups other than Type; a label
 * of an exclusive group replaces its sibling (the server enforces it). With
 * text that names no label, offers "Create label".
 */
export function LabelPicker({ labels, selected, onToggle, onCreate, onClose }: Props) {
  const { t } = useTranslation('issues')
  const [q, setQ] = useState('')
  const needle = q.trim().toLowerCase()
  const all: Item[] = labels.top
    .filter((l) => l.id !== labels.typeGroupId)
    .flatMap((l): Item[] =>
      l.isGroup
        ? l.children.map((c) => ({ key: c.id, label: c.name, group: l.name, target: c }))
        : [{ key: l.id, label: l.name, target: l }],
    )
  const shown = needle ? all.filter((i) => `${i.group ?? ''} ${i.label}`.toLowerCase().includes(needle)) : all
  const exact = all.some((i) => !i.group && i.label.toLowerCase() === needle)
  const items: Item[] = needle && !exact ? [...shown, { key: `create:${q.trim()}`, label: t('labels.create', { name: q.trim() }), create: q.trim() }] : shown
  const isOn = (i: Item) => !!i.target && selected.includes(i.target.id)
  return (
    <Picker
      items={items}
      getKey={(i) => i.key}
      getLabel={(i) => i.label}
      isSelected={isOn}
      onQuery={setQ}
      renderItem={(i) =>
        i.create !== undefined ? (
          <>
            <Plus size={14} aria-hidden />
            <span>{i.label}</span>
          </>
        ) : (
          <>
            <span className={s.dot} style={{ background: i.target?.color }} aria-hidden />
            {i.group && <span className={s.group}>{i.group}</span>}
            <span>{i.label}</span>
            {isOn(i) && <Check size={14} className={s.check} aria-hidden />}
          </>
        )
      }
      onSelect={(i) => {
        if (i.create !== undefined) onCreate(i.create)
        else if (i.target) onToggle(i.target, !isOn(i))
      }}
      onClose={onClose}
      placeholder={t('labels.labels')}
    />
  )
}
