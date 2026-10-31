import { closestCorners, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors } from '@dnd-kit/core'
import { sortableKeyboardCoordinates } from '@dnd-kit/sortable'
import { useRef, useState } from 'react'
import { useNavigate } from 'react-router'

import { isInteractive, useHotkey } from '../../lib/hotkeys'
import { usePalette } from '../command/palette'
import { targetsFrom } from '../command/targets'
import type { PickerState } from '../pickers/StatePicker'
import { useCreateIssue } from '../issues/createIssue'
import { toFilter, toSearch, today, type ViewParams } from '../issues/filters'
import s from './board.module.css'
import { BoardColumn } from './BoardColumn'
import { useBoardDrag } from './useBoardDrag'

type Props = {
  /** milestoneId: only that milestone's issues. */
  project: { id: string; key: string; states: PickerState[]; archived: boolean; milestoneId?: string }
  params: ViewParams
  viewerId: string
}

/** Columns of the project's states; drag cards; J/K in a column, H/L across, Enter opens. */
export function Board({ project, params, viewerId }: Props) {
  const navigate = useNavigate()
  const { openCreateIssue } = useCreateIssue()
  const container = useRef<HTMLDivElement>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const search = toSearch({ ...params, order: 'SORT_ORDER', dir: 'ASC' })
  // A milestone's board keeps its own cache next to the project board.
  const cacheKey = project.milestoneId ? `${project.key}/m/${project.milestoneId}` : project.key
  const { onDragEnd } = useBoardDrag(cacheKey, search)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))
  const base = { ...toFilter(params, today(), viewerId), ...(project.milestoneId ? { milestone: { eq: project.milestoneId } } : {}) }
  const states = [...project.states]
    .sort((a, b) => a.position - b.position)
    .filter((st) => (!params.state.length || params.state.includes(st.id)) && (!params.type.length || params.type.includes(st.type)))

  const columns = () =>
    [...(container.current?.querySelectorAll<HTMLElement>('[data-column]') ?? [])].map((c) => [...c.querySelectorAll<HTMLElement>('[data-row-id]')])
  const position = () => {
    const cols = columns()
    for (let c = 0; c < cols.length; c++) {
      const r = cols[c].findIndex((el) => el.dataset.rowId === selected)
      if (r >= 0) return { cols, c, r }
    }
    return { cols, c: -1, r: -1 }
  }
  const pick = (el: HTMLElement | undefined) => {
    if (!el) return
    setSelected(el.dataset.rowId ?? null)
    el.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
  }
  const vertical = (delta: number) => {
    const { cols, c, r } = position()
    if (c < 0) return pick(cols.find((col) => col.length)?.[0])
    pick(cols[c][Math.min(Math.max(r + delta, 0), cols[c].length - 1)])
  }
  const horizontal = (delta: number) => {
    const { cols, c, r } = position()
    if (c < 0) return pick(cols.find((col) => col.length)?.[0])
    for (let n = c + delta; n >= 0 && n < cols.length; n += delta) {
      if (cols[n].length) return pick(cols[n][Math.min(r, cols[n].length - 1)])
    }
  }
  useHotkey('j', () => vertical(1))
  useHotkey('k', () => vertical(-1))
  useHotkey('l', () => horizontal(1))
  useHotkey('h', () => horizontal(-1))
  const palette = usePalette()
  // L moves between columns here: labels are in the palette.
  const openPage = (page: 'state' | 'assignee' | 'priority' | 'type' | 'milestone') => {
    const t = targetsFrom(position().cols.flat().filter((el) => el.dataset.rowId === selected))
    if (!t.length || project.archived) return false
    palette.open(page, t)
  }
  useHotkey('s', () => openPage('state'))
  useHotkey('a', () => openPage('assignee'))
  useHotkey('p', () => openPage('priority'))
  useHotkey('t', () => openPage('type'))
  useHotkey('m', () => openPage('milestone'))
  useHotkey('enter', (e) => {
    if (isInteractive(e.target)) return false
    const href = position().cols.flat().find((el) => el.dataset.rowId === selected)?.dataset.href
    if (!href) return false
    navigate(href)
  })

  return (
    <DndContext sensors={sensors} collisionDetection={closestCorners} onDragEnd={onDragEnd}>
      <div ref={container} className={s.board}>
        {states.map((st) => (
          <BoardColumn
            key={st.id}
            projectKey={cacheKey}
            state={st}
            filter={{ ...base, project: { eq: project.id }, state: { eq: st.id } }}
            search={search}
            selected={selected}
            onSelect={setSelected}
            draggable={!project.archived}
            onCreate={project.archived ? undefined : () => openCreateIssue({ projectId: project.id, stateId: st.id })}
          />
        ))}
      </div>
    </DndContext>
  )
}
