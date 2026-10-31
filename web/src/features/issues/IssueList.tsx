import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router'
import { useTranslation } from 'react-i18next'

import type { IssueFilter, IssueOrder, StateType } from '../../gql/graphql'
import { StateIcon } from '../../ui/StateIcon'
import type { PickerUser } from '../pickers/AssigneePicker'
import type { PickerState } from '../pickers/StatePicker'
import { BulkBar } from './BulkBar'
import { hasFilters, toFilter, toSearch, today, type ViewParams } from './filters'
import { IssueGroup } from './IssueGroup'
import s from './issues.module.css'
import { SelectionContext } from './selection'
import { useSelection } from './useSelection'
import { viewMemory } from './viewMemory'

export type ListScope =
  | { kind: 'project'; projectId: string; projectKey: string; states: PickerState[]; archived: boolean; milestoneId?: string }
  | { kind: 'my' }

const stateTypes: StateType[] = ['BACKLOG', 'UNSTARTED', 'STARTED', 'COMPLETED', 'CANCELED']

type Group = { id: string; label: string; icon: ReactNode; filter: IssueFilter; type: StateType }

type Props = {
  scope: ListScope
  params: ViewParams
  users: PickerUser[]
  viewerId: string
  /** Shown instead of the groups when the project has no issues at all. */
  empty?: ReactNode
  /** Header action of a group, e.g. "+" to create in that state. */
  groupAction?: (stateId: string) => ReactNode
}

/** Issues grouped by state (project) or state type (My issues). */
export function IssueList({ scope, params, users, viewerId, empty, groupAction }: Props) {
  const { t } = useTranslation(['issues', 'common'])
  const container = useRef<HTMLDivElement>(null)
  const archived = scope.kind === 'project' && scope.archived
  const { key: entryKey } = useLocation()
  const remembered = viewMemory.get(entryKey)
  const sel = useSelection(container, { checkable: !archived, initialSelected: remembered?.selected ?? null })
  const selectedRef = useRef(sel.selected)
  useEffect(() => {
    selectedRef.current = sel.selected
  })
  // Remember where the user was when leaving (opening an issue, switching pages).
  useEffect(() => {
    const main = container.current?.closest('main')
    return () => {
      viewMemory.set(entryKey, { scroll: main?.scrollTop ?? 0, selected: selectedRef.current })
    }
  }, [entryKey])
  const [counts, setCounts] = useState<Record<string, number>>({})
  const onCount = useCallback((id: string, n: number) => setCounts((c) => (c[id] === n ? c : { ...c, [id]: n })), [])

  const filtered = hasFilters(params)
  // Nesting shows top-level issues only, so it applies to an unfiltered project
  // list; with filters, and on My issues, matching sub-issues list flat.
  // A milestone holds sub-issues of other parents too: its list is flat.
  const nested = params.nest && scope.kind === 'project' && !filtered && !scope.milestoneId
  const search = toSearch(params)
  const orderBy: IssueOrder = { field: params.order, direction: params.dir }
  const groups = useMemo((): Group[] => {
    const base = toFilter(params, today(), viewerId)
    if (nested) base.parent = { null: true }
    if (scope.kind === 'project') {
      return [...scope.states]
        .sort((a, b) => a.position - b.position)
        .filter((st) => !params.state.length || params.state.includes(st.id))
        .filter((st) => !params.type.length || params.type.includes(st.type))
        .map((st) => ({
          id: st.id, label: st.name, type: st.type,
          icon: <StateIcon type={st.type} color={st.color} />,
          filter: {
            ...base, project: { eq: scope.projectId }, state: { eq: st.id },
            ...(scope.milestoneId ? { milestone: { eq: scope.milestoneId } } : {}),
          },
        }))
    }
    return stateTypes
      .filter((ty) => !params.type.length || params.type.includes(ty))
      .map((ty) => ({
        id: ty, type: ty,
        label: t(`common:stateType.${ty.toLowerCase() as Lowercase<StateType>}`),
        icon: <StateIcon type={ty} color="var(--fg-muted)" />,
        filter: { ...base, assignee: { isMe: true }, stateType: { ...base.stateType, eq: ty } },
      }))
  }, [scope, params, viewerId, t, nested])

  const loaded = groups.every((g) => counts[g.id] !== undefined)
  const none = loaded && groups.every((g) => counts[g.id] === 0)
  const scopeKey = scope.kind === 'project' ? (scope.milestoneId ? `${scope.projectKey}/m/${scope.milestoneId}` : scope.projectKey) : 'my'

  // Restore the scroll offset once the groups have their rows.
  const restored = useRef(false)
  useEffect(() => {
    if (!loaded || restored.current) return
    restored.current = true
    const main = container.current?.closest('main')
    const saved = viewMemory.get(entryKey)
    if (main && saved) main.scrollTop = saved.scroll
  }, [loaded, entryKey])

  return (
    <SelectionContext.Provider value={{ ...sel, checkable: !archived }}>
      <div ref={container} className={s.list}>
        {none && !filtered && empty}
        {none && filtered && <p className={s.emptyText}>{t('list.noMatches')}</p>}
        {/* Groups stay mounted while hidden, so their queries refetch when issues appear. */}
        <div hidden={none && !filtered && !!empty}>
          {groups.map((g) => (
            <IssueGroup
              key={g.id}
              scope={scopeKey}
              groupId={g.id}
              search={search}
              label={g.label}
              icon={g.icon}
              filter={g.filter}
              orderBy={orderBy}
              nested={nested}
              hideEmpty={filtered}
              collapseFor={(n) => g.type === 'CANCELED' || (g.type === 'COMPLETED' && n > 10)}
              onCount={onCount}
              action={scope.kind === 'project' && !archived ? groupAction?.(g.id) : undefined}
            />
          ))}
        </div>
        {!loaded && <Skeleton />}
      </div>
      {sel.checked.size > 0 && !archived && (
        <BulkBar ids={[...sel.checked]} states={scope.kind === 'project' ? scope.states : undefined} users={users} viewerId={viewerId} onDone={sel.clear} />
      )}
    </SelectionContext.Provider>
  )
}

function Skeleton() {
  return (
    <div aria-hidden>
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className={s.skeleton} />
      ))}
    </div>
  )
}
