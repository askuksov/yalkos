import { ListFilter, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { StateType } from '../../gql/graphql'
import { formatOr } from '../../i18n'
import { useHotkey } from '../../lib/hotkeys'
import { Button } from '../../ui/Button'
import { Picker } from '../../ui/Picker'
import { Popover } from '../../ui/Popover'
import { priorityKey } from '../../ui/priority'
import { PriorityIcon } from '../../ui/PriorityIcon'
import { StateIcon } from '../../ui/StateIcon'
import type { PickerState } from '../pickers/StatePicker'
import type { PickerUser } from '../pickers/AssigneePicker'
import ls from '../labels/labels.module.css'
import type { LabelCatalog } from '../labels/useLabels'
import { isDate, type ViewParams } from './filters'
import s from './issues.module.css'

const stateTypes: StateType[] = ['BACKLOG', 'UNSTARTED', 'STARTED', 'COMPLETED', 'CANCELED']
const priorities = [0, 1, 2, 3, 4]

type Scope = {
  params: ViewParams
  onChange: (next: ViewParams) => void
  /** Project scope: its states (the State filter is hidden without them). */
  states?: PickerState[]
  users: PickerUser[]
  viewerId: string
  /** My issues: the assignee is always the viewer. */
  hideAssignee?: boolean
  /** Workspace labels (types and labels filters; hidden until loaded). */
  labels?: LabelCatalog
  /** Project scope: its milestones (the Milestone filter is hidden without them). */
  milestones?: { id: string; name: string }[]
}

type Category = 'state' | 'type' | 'kind' | 'label' | 'nlabel' | 'milestone' | 'assignee' | 'priority' | 'due'

function toggle<T>(list: T[], v: T): T[] {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v]
}

/** Text filter (F focuses it) and the Filter menu, for the page header. */
export function FilterBar({ params, onChange, states, users, viewerId, hideAssignee, labels, milestones }: Scope) {
  const { t } = useTranslation(['issues', 'common'])
  const input = useRef<HTMLInputElement>(null)
  const [text, setText] = useState(params.q ?? '')
  const [seenQ, setSeenQ] = useState(params.q)
  if (params.q !== seenQ) {
    // The URL changed elsewhere (chip removed, back button): follow it.
    setSeenQ(params.q)
    setText(params.q ?? '')
  }
  useEffect(() => {
    const q = text.trim() || undefined
    if (q === params.q) return
    const id = setTimeout(() => onChange({ ...params, q }), 250)
    return () => clearTimeout(id)
  }, [text, params, onChange])
  useHotkey('f', () => input.current?.focus())

  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const [open, setOpen] = useState(false)
  const [category, setCategory] = useState<Category | null>(null)
  const [before, setBefore] = useState('')
  const close = () => {
    setOpen(false)
    setCategory(null)
  }
  const apply = (next: ViewParams) => {
    onChange(next)
    close()
  }

  type Entry = { key: string; label: string; run: () => void }
  const categories: Entry[] = [
    ...(states ? [{ key: 'state', label: t('filter.categories.state'), run: () => setCategory('state') }] : []),
    { key: 'type', label: t('filter.categories.type'), run: () => setCategory('type') },
    ...(hideAssignee ? [] : [{ key: 'assignee', label: t('filter.categories.assignee'), run: () => setCategory('assignee') }]),
    { key: 'priority', label: t('filter.categories.priority'), run: () => setCategory('priority') },
    ...(labels
      ? [
          { key: 'kind', label: t('filter.categories.kind'), run: () => setCategory('kind') },
          { key: 'label', label: t('filter.categories.label'), run: () => setCategory('label') },
          { key: 'nlabel', label: t('filter.categories.nlabel'), run: () => setCategory('nlabel') },
          { key: 'nolabel', label: t('filter.categories.nolabel'), run: () => apply({ ...params, nolabel: params.nolabel ? undefined : true, label: [], nlabel: [] }) },
        ]
      : []),
    ...(milestones ? [{ key: 'milestone', label: t('filter.categories.milestone'), run: () => setCategory('milestone') }] : []),
    { key: 'due', label: t('filter.categories.due'), run: () => setCategory('due') },
    { key: 'children', label: t('filter.categories.children'), run: () => apply({ ...params, children: params.children ? undefined : true }) },
    { key: 'parent', label: t('filter.categories.parent'), run: () => apply({ ...params, parent: params.parent ? undefined : 'none' }) },
  ]

  const assigneeItems = [
    { key: 'me', label: t('common:pickers.me') },
    ...users.filter((u) => !u.disabled && u.id !== viewerId).map((u) => ({ key: u.id, label: u.name })),
    { key: 'none', label: t('common:pickers.noAssignee') },
  ]
  const kindItems = [...(labels?.types ?? []).map((l) => ({ key: l.id, label: l.name, color: l.color })), { key: 'none', label: t('labels.noType'), color: '' }]
  const labelItems = (labels?.top ?? [])
    .filter((l) => l.id !== labels?.typeGroupId)
    .flatMap((l) => (l.isGroup ? l.children.map((c) => ({ key: c.id, label: `${l.name} › ${c.name}`, color: c.color })) : [{ key: l.id, label: l.name, color: l.color }]))
  const dueItems = (['overdue', 'week', 'none'] as const).map((k) => ({ key: k, label: t(`filter.due.${k}`) }))

  return (
    <div className={s.toolbar}>
      <input
        ref={input}
        className={s.search}
        type="search"
        aria-label={t('filter.text')}
        placeholder={t('filter.text')}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') e.currentTarget.blur()
        }}
      />
      <Button ref={setAnchor} size="sm" icon={<ListFilter size={14} aria-hidden />} aria-haspopup="dialog" onClick={() => (open ? close() : setOpen(true))}>
        {t('filter.button')}
      </Button>
      <Popover anchor={anchor} open={open} onClose={close} placement="bottom-end">
        {category === null && (
          <Picker items={categories} getKey={(c) => c.key} getLabel={(c) => c.label} onSelect={(c) => c.run()} onClose={close} placeholder={t('filter.button')} />
        )}
        {category === 'state' && states && (
          <Picker
            items={[...states].sort((a, b) => a.position - b.position)}
            getKey={(x) => x.id}
            getLabel={(x) => x.name}
            renderItem={(x) => (
              <>
                <StateIcon type={x.type} color={x.color} />
                <span>{x.name}</span>
              </>
            )}
            onSelect={(x) => apply({ ...params, state: toggle(params.state, x.id) })}
            onClose={close}
            placeholder={t('filter.categories.state')}
          />
        )}
        {category === 'type' && (
          <Picker
            items={stateTypes}
            getKey={String}
            getLabel={(x) => t(`common:stateType.${x.toLowerCase() as Lowercase<StateType>}`)}
            renderItem={(x) => (
              <>
                <StateIcon type={x} color="var(--fg-muted)" />
                <span>{t(`common:stateType.${x.toLowerCase() as Lowercase<StateType>}`)}</span>
              </>
            )}
            onSelect={(x) => apply({ ...params, type: toggle(params.type, x) })}
            onClose={close}
            placeholder={t('filter.categories.type')}
          />
        )}
        {category === 'kind' && (
          <Picker
            items={kindItems}
            getKey={(x) => x.key}
            getLabel={(x) => x.label}
            renderItem={(x) => (
              <>
                {x.color && <span className={ls.dot} style={{ background: x.color }} aria-hidden />}
                <span>{x.label}</span>
              </>
            )}
            onSelect={(x) => {
              // "No type" excludes types and the other way round.
              const next = x.key === 'none' ? (params.kind.includes('none') ? [] : ['none']) : toggle(params.kind.filter((k) => k !== 'none'), x.key)
              apply({ ...params, kind: next })
            }}
            onClose={close}
            placeholder={t('filter.categories.kind')}
          />
        )}
        {category === 'label' && (
          <Picker
            items={labelItems}
            getKey={(x) => x.key}
            getLabel={(x) => x.label}
            renderItem={(x) => (
              <>
                <span className={ls.dot} style={{ background: x.color }} aria-hidden />
                <span>{x.label}</span>
              </>
            )}
            onSelect={(x) => apply({ ...params, label: toggle(params.label, x.key), nolabel: undefined })}
            onClose={close}
            placeholder={t('filter.categories.label')}
          />
        )}
        {category === 'nlabel' && (
          <Picker
            items={labelItems}
            getKey={(x) => x.key}
            getLabel={(x) => x.label}
            renderItem={(x) => (
              <>
                <span className={ls.dot} style={{ background: x.color }} aria-hidden />
                <span>{x.label}</span>
              </>
            )}
            onSelect={(x) => apply({ ...params, nlabel: toggle(params.nlabel, x.key), nolabel: undefined })}
            onClose={close}
            placeholder={t('filter.categories.nlabel')}
          />
        )}
        {category === 'milestone' && milestones && (
          <Picker
            items={[...milestones.map((m) => ({ key: m.id, label: m.name })), { key: 'none', label: t('milestones.none') }]}
            getKey={(x) => x.key}
            getLabel={(x) => x.label}
            onSelect={(x) => apply({ ...params, milestone: params.milestone === x.key ? undefined : x.key })}
            onClose={close}
            placeholder={t('filter.categories.milestone')}
          />
        )}
        {category === 'assignee' && (
          <Picker
            items={assigneeItems}
            getKey={(x) => x.key}
            getLabel={(x) => x.label}
            onSelect={(x) => {
              // "No assignee" excludes people and the other way round (conditions are AND-ed).
              const next = x.key === 'none' ? (params.assignee.includes('none') ? [] : ['none']) : toggle(params.assignee.filter((a) => a !== 'none'), x.key)
              apply({ ...params, assignee: next })
            }}
            onClose={close}
            placeholder={t('filter.categories.assignee')}
          />
        )}
        {category === 'priority' && (
          <Picker
            items={priorities}
            getKey={String}
            getLabel={(p) => t(`common:priority.${priorityKey(p)}`)}
            renderItem={(p) => (
              <>
                <PriorityIcon priority={p} decorative />
                <span>{t(`common:priority.${priorityKey(p)}`)}</span>
              </>
            )}
            onSelect={(p) => apply({ ...params, priority: toggle(params.priority, p) })}
            onClose={close}
            placeholder={t('filter.categories.priority')}
          />
        )}
        {category === 'due' && (
          <div>
            <Picker items={dueItems} getKey={(x) => x.key} getLabel={(x) => x.label} onSelect={(x) => apply({ ...params, due: x.key })} onClose={close} placeholder={t('filter.categories.due')} />
            <form
              className={s.dateRow}
              onSubmit={(e) => {
                e.preventDefault()
                if (isDate(before)) apply({ ...params, due: before })
              }}
            >
              <input type="date" aria-label={t('filter.due.before')} value={before} onChange={(e) => setBefore(e.target.value)} />
              <Button type="submit" size="sm" disabled={!isDate(before)}>
                {t('filter.due.before')}
              </Button>
            </form>
          </div>
        )}
      </Popover>
    </div>
  )
}

/** Active filters as removable chips (under the header). */
export function FilterChips({ params, onChange, states, users, viewerId, labels, milestones }: Scope) {
  const { t } = useTranslation(['issues', 'common'])
  const labelName = (id: string) => (id === 'none' ? t('labels.noType') : (labels?.byId.get(id)?.name ?? id))
  const userName = (id: string) =>
    id === 'me' || id === viewerId ? t('common:pickers.me') : id === 'none' ? t('common:pickers.noAssignee') : (users.find((u) => u.id === id)?.name ?? id)
  const chips: { key: string; name: string; values?: string[]; without: ViewParams }[] = []
  if (params.state.length)
    chips.push({ key: 'state', name: t('filter.categories.state'), values: params.state.map((id) => states?.find((x) => x.id === id)?.name ?? id), without: { ...params, state: [] } })
  if (params.type.length)
    chips.push({
      key: 'type', name: t('filter.categories.type'),
      values: params.type.map((x) => t(`common:stateType.${x.toLowerCase() as Lowercase<StateType>}`)), without: { ...params, type: [] },
    })
  if (params.assignee.length)
    chips.push({ key: 'assignee', name: t('filter.categories.assignee'), values: params.assignee.map(userName), without: { ...params, assignee: [] } })
  if (params.priority.length)
    chips.push({
      key: 'priority', name: t('filter.categories.priority'),
      values: params.priority.map((p) => t(`common:priority.${priorityKey(p)}`)), without: { ...params, priority: [] },
    })
  if (params.kind.length)
    chips.push({ key: 'kind', name: t('filter.categories.kind'), values: params.kind.map(labelName), without: { ...params, kind: [] } })
  if (params.label.length)
    chips.push({ key: 'label', name: t('filter.categories.label'), values: params.label.map(labelName), without: { ...params, label: [] } })
  if (params.milestone) {
    const name = params.milestone === 'none' ? t('milestones.none') : (milestones?.find((m) => m.id === params.milestone)?.name ?? params.milestone)
    chips.push({ key: 'milestone', name: t('filter.categories.milestone'), values: [name], without: { ...params, milestone: undefined } })
  }
  if (params.nlabel.length)
    chips.push({ key: 'nlabel', name: t('filter.categories.nlabel'), values: params.nlabel.map(labelName), without: { ...params, nlabel: [] } })
  if (params.nolabel) chips.push({ key: 'nolabel', name: t('filter.categories.nolabel'), without: { ...params, nolabel: undefined } })
  if (params.due) {
    const label = isDate(params.due) ? t('filter.due.beforeDate', { date: params.due }) : t(`filter.due.${params.due as 'overdue' | 'week' | 'none'}`)
    chips.push({ key: 'due', name: t('filter.categories.due'), values: [label], without: { ...params, due: undefined } })
  }
  if (params.children) chips.push({ key: 'children', name: t('filter.categories.children'), without: { ...params, children: undefined } })
  if (params.parent) chips.push({ key: 'parent', name: t('filter.categories.parent'), without: { ...params, parent: undefined } })
  if (!chips.length) return null
  return (
    <div className={s.chips}>
      {chips.map((c) => {
        const text = c.values ? t('filter.chip', { name: c.name, values: formatOr(c.values) }) : c.name
        return (
          <span key={c.key} className={s.chip} title={text}>
            <span className={s.chipText}>{text}</span>
            <button type="button" className={s.chipRemove} aria-label={t('filter.remove', { name: c.name })} onClick={() => onChange(c.without)}>
              <X size={12} aria-hidden />
            </button>
          </span>
        )
      })}
      {chips.length > 1 && (
        <Button size="sm" variant="ghost" onClick={() =>
            onChange({ ...params, state: [], type: [], assignee: [], priority: [], kind: [], label: [], nlabel: [], nolabel: undefined, milestone: undefined, due: undefined, children: undefined, parent: undefined })
          }>
          {t('filter.clear')}
        </Button>
      )}
    </div>
  )
}
