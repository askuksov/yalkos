import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Command } from 'cmdk'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'

import { errorMessage, request } from '../../api/client'
import { useMembers, useProject, useProjects, useViewer } from '../../api/hooks'
import { compareNames } from '../../i18n'
import { useIssueMutation } from '../../lib/issueCache'
import { Avatar } from '../../ui/Avatar'
import { Button } from '../../ui/Button'
import { priorityKey } from '../../ui/priority'
import { PriorityIcon } from '../../ui/PriorityIcon'
import { StateIcon } from '../../ui/StateIcon'
import { useToast } from '../../ui/useToast'
import { MoveIssueMutation } from '../issue-page/operations'
import { DeleteIssueMutation } from '../issues/fragments'
import s from './command.module.css'
import type { IssueTarget } from './recent'
import { PaletteChildCountQuery, PaletteSearchQuery, useDebounced } from './search'

type PageProps = { q: string; targets: IssueTarget[]; done: () => void }

const has = (label: string, q: string) => label.toLowerCase().includes(q.trim().toLowerCase())

/** The one project all targets share, or null. */
function sharedProject(targets: IssueTarget[]): string | null {
  const ids = new Set(targets.map((x) => x.projectId))
  return ids.size === 1 ? [...ids][0] : null
}

export function StatePage({ q, targets, done }: PageProps) {
  const update = useIssueMutation()
  const project = useProject(sharedProject(targets) ?? undefined).data
  const states = [...(project?.states ?? [])].sort((a, b) => a.position - b.position).filter((x) => has(x.name, q))
  return (
    <>
      {states.map((st) => (
        <Command.Item
          key={st.id}
          value={st.id}
          onSelect={() => {
            targets.forEach((x) => update.mutate({ id: x.id, input: { stateId: st.id }, optimistic: { state: { id: st.id, name: st.name, type: st.type, color: st.color } } }))
            done()
          }}
        >
          <StateIcon type={st.type} color={st.color} />
          <span className={s.label}>{st.name}</span>
        </Command.Item>
      ))}
    </>
  )
}

export function AssigneePage({ q, targets, done }: PageProps) {
  const { t } = useTranslation()
  const update = useIssueMutation()
  const viewer = useViewer().data
  const users = useMembers().data ?? []
  const items = [
    ...(viewer ? [{ key: viewer.id, label: t('pickers.me'), user: { id: viewer.id, name: viewer.name } as { id: string; name: string } | null }] : []),
    ...users.filter((u) => !u.disabled && u.id !== viewer?.id).sort((a, b) => compareNames(a.name, b.name)).map((u) => ({ key: u.id, label: u.name, user: { id: u.id, name: u.name } })),
    { key: 'none', label: t('pickers.noAssignee'), user: null },
  ].filter((i) => has(i.label, q))
  return (
    <>
      {items.map((i) => (
        <Command.Item
          key={i.key}
          value={i.key}
          onSelect={() => {
            targets.forEach((x) => update.mutate({ id: x.id, input: { assigneeId: i.user?.id ?? null }, optimistic: { assignee: i.user } }))
            done()
          }}
        >
          {i.user ? <Avatar id={i.user.id} name={i.user.name} size={18} decorative /> : null}
          <span className={s.label}>{i.label}</span>
        </Command.Item>
      ))}
    </>
  )
}

export function PriorityPage({ q, targets, done }: PageProps) {
  const { t } = useTranslation()
  const update = useIssueMutation()
  return (
    <>
      {[0, 1, 2, 3, 4]
        .filter((p) => has(t(`priority.${priorityKey(p)}`), q))
        .map((p) => (
          <Command.Item
            key={p}
            value={String(p)}
            onSelect={() => {
              targets.forEach((x) => update.mutate({ id: x.id, input: { priority: p }, optimistic: { priority: p } }))
              done()
            }}
          >
            <PriorityIcon priority={p} decorative />
            <span className={s.label}>{t(`priority.${priorityKey(p)}`)}</span>
          </Command.Item>
        ))}
    </>
  )
}

export function DuePage({ targets, done }: PageProps) {
  const { t } = useTranslation('command')
  const update = useIssueMutation()
  const [date, setDate] = useState('')
  const apply = (dueDate: string | null) => {
    targets.forEach((x) => update.mutate({ id: x.id, input: { dueDate }, optimistic: { dueDate } }))
    done()
  }
  return (
    <>
      <form
        className={s.dateForm}
        onSubmit={(e) => {
          e.preventDefault()
          if (date) apply(date)
        }}
      >
        <input type="date" aria-label={t('due.set')} value={date} onChange={(e) => setDate(e.target.value)} />
        <Button type="submit" size="sm" disabled={!date}>
          {t('due.set')}
        </Button>
      </form>
      <Command.Item value="clear" onSelect={() => apply(null)}>
        <span className={s.label}>{t('due.clear')}</span>
      </Command.Item>
    </>
  )
}

export function ParentPage({ q, targets, done }: PageProps) {
  const { t } = useTranslation('command')
  const update = useIssueMutation()
  const projectId = sharedProject(targets)
  const search = useDebounced(q.trim(), 150)
  const issues = useQuery({
    queryKey: ['paletteParent', projectId, search],
    queryFn: () => request(PaletteSearchQuery, { filter: { project: { eq: projectId }, ...(search ? { search } : {}) }, first: 20 }),
    enabled: !!projectId,
    placeholderData: keepPreviousData,
  })
  const exclude = new Set(targets.map((x) => x.id))
  return (
    <>
      <Command.Item
        value="none"
        onSelect={() => {
          targets.forEach((x) => update.mutate({ id: x.id, input: { parentId: null }, optimistic: { parent: null } }))
          done()
        }}
      >
        <span className={s.label}>{t('noParent')}</span>
      </Command.Item>
      {(issues.data?.issues.nodes ?? [])
        .filter((i) => !exclude.has(i.id))
        .map((i) => (
          <Command.Item
            key={i.id}
            value={i.id}
            onSelect={() => {
              targets.forEach((x) => update.mutate({ id: x.id, input: { parentId: i.id }, optimistic: { parent: { id: i.id, identifier: i.identifier } } }))
              done()
            }}
          >
            <span className={s.identifier}>{i.identifier}</span>
            <span className={s.label}>{i.title}</span>
          </Command.Item>
        ))}
    </>
  )
}

export function MovePage({ q, targets, done }: PageProps) {
  const { t } = useTranslation(['command', 'issue'])
  const qc = useQueryClient()
  const toast = useToast()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const projects = useProjects().data ?? []
  const [target, setTarget] = useState<{ id: string; key: string } | null>(null)
  const issue = targets[0]
  // Sub-issues move too; the confirm says so.
  const childCount = useQuery({
    queryKey: ['paletteChildCount', issue?.id],
    queryFn: () => request(PaletteChildCountQuery, { id: issue?.id ?? '' }),
    enabled: !!issue,
    select: (d) => d.issue.children.totalCount,
  })
  const move = useMutation({
    mutationFn: (projectId: string) => request(MoveIssueMutation, { id: issue.id, projectId }),
    onSuccess: (d) => {
      void Promise.all(['issues', 'children', 'issue', 'history'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
      if (pathname === `/issue/${issue.identifier}`) navigate(`/issue/${d.moveIssue.identifier}`, { replace: true })
      done()
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })
  if (!issue) return null
  if (target) {
    return (
      <Command.Item value="confirm" onSelect={() => move.mutate(target.id)}>
        <span className={s.label}>
          {t(childCount.data ? 'issue:move.confirmWithChildren' : 'issue:move.confirm', { identifier: issue.identifier, key: target.key })}
        </span>
      </Command.Item>
    )
  }
  return (
    <>
      {projects
        .filter((p) => p.id !== issue.projectId && !p.archivedAt && has(`${p.key} ${p.name}`, q))
        .sort((a, b) => compareNames(a.name, b.name))
        .map((p) => (
          <Command.Item key={p.id} value={p.id} onSelect={() => setTarget({ id: p.id, key: p.key })}>
            <span className={s.identifier}>{p.key}</span>
            <span className={s.label}>{p.name}</span>
          </Command.Item>
        ))}
    </>
  )
}

export function DeletePage({ targets, done }: PageProps) {
  const { t } = useTranslation('command')
  const qc = useQueryClient()
  const toast = useToast()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const remove = useMutation({
    mutationFn: () => Promise.all(targets.map((x) => request(DeleteIssueMutation, { id: x.id }))),
    onSuccess: () => {
      toast.show(t('deleted', { count: targets.length }))
      void Promise.all(['issues', 'children'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
      const open = targets.find((x) => pathname === `/issue/${x.identifier}`)
      if (open) navigate(open.projectKey ? `/p/${open.projectKey}` : '/my-issues', { replace: true })
      done()
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })
  return (
    <Command.Item value="confirm" onSelect={() => remove.mutate()}>
      <span className={s.label}>{t('delete.confirm', { count: targets.length })}</span>
    </Command.Item>
  )
}
