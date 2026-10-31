import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CalendarDays, CornerDownRight, FolderKanban, X } from 'lucide-react'
import { type FormEvent, type ReactNode, useId, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { errorMessage, GraphQLRequestError, request } from '../../api/client'
import { useMembers, useProject, useViewer } from '../../api/hooks'
import { graphql } from '../../gql'
import type { CreateIssueInput } from '../../gql/graphql'
import { useHotkey } from '../../lib/hotkeys'
import { Avatar } from '../../ui/Avatar'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { Popover } from '../../ui/Popover'
import { priorityKey } from '../../ui/priority'
import { PriorityIcon } from '../../ui/PriorityIcon'
import { StateIcon } from '../../ui/StateIcon'
import { useToast } from '../../ui/useToast'
import { AssigneePicker } from '../pickers/AssigneePicker'
import { IssuePicker } from '../pickers/IssuePicker'
import { PriorityPicker } from '../pickers/PriorityPicker'
import { ProjectPicker } from '../pickers/ProjectPicker'
import { StatePicker } from '../pickers/StatePicker'
import { CreateIssueContext, type CreateIssueApi, type CreateIssueOptions } from './createIssue'
import s from './issues.module.css'

const CreateIssueMutation = graphql(`
  mutation CreateIssue($input: CreateIssueInput!) {
    createIssue(input: $input) { id identifier project { id } }
  }
`)

const lastProjectKey = 'yalkos.lastProject'

function lastProject(): string | undefined {
  try {
    return localStorage.getItem(lastProjectKey) ?? undefined
  } catch {
    return undefined
  }
}

/** Provides openCreateIssue and the C shortcut; renders the dialog. */
export function CreateIssueProvider({ children }: { children: ReactNode }) {
  const [opts, setOpts] = useState<CreateIssueOptions | null>(null)
  const defaults = useRef<CreateIssueOptions | null>(null)
  const api = useMemo<CreateIssueApi>(
    () => ({
      openCreateIssue: (o = {}) => setOpts(o),
      setPageDefaults: (o) => {
        defaults.current = o
      },
      openForPage: () => setOpts(defaults.current ?? {}),
    }),
    [],
  )
  useHotkey('c', () => api.openForPage(), { enabled: opts === null })
  const { t } = useTranslation('issues')
  return (
    <CreateIssueContext.Provider value={api}>
      {children}
      <Dialog open={opts !== null} onClose={() => setOpts(null)} title={t('create.title')} wide>
        {opts && <CreateForm opts={opts} onClose={() => setOpts(null)} />}
      </Dialog>
    </CreateIssueContext.Provider>
  )
}

type Parent = { id: string; identifier: string } | null
type Open = 'project' | 'state' | 'priority' | 'assignee' | 'parent' | null

function CreateForm({ opts, onClose }: { opts: CreateIssueOptions; onClose: () => void }) {
  const { t } = useTranslation(['issues', 'common'])
  const qc = useQueryClient()
  const toast = useToast()
  const navigate = useNavigate()
  const viewer = useViewer().data
  const users = useMembers().data ?? []
  const ids = { title: useId(), desc: useId(), titleError: useId(), formError: useId() }
  const titleRef = useRef<HTMLInputElement>(null)

  const [projectId, setProjectId] = useState(opts.projectId ?? (opts.parentId ? undefined : lastProject()))
  const project = useProject(projectId).data
  const [stateId, setStateId] = useState<string | undefined>(opts.stateId)
  const [priority, setPriority] = useState(0)
  const [assigneeId, setAssigneeId] = useState<string | null>(null)
  const [dueDate, setDueDate] = useState('')
  const [parent, setParent] = useState<Parent>(opts.parentId ? { id: opts.parentId, identifier: opts.parentIdentifier ?? '' } : null)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [createMore, setCreateMore] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [open, setOpen] = useState<Open>(null)
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)

  const state = project?.states.find((x) => x.id === stateId) ?? project?.states.find((x) => x.isDefault)
  const assignee = users.find((u) => u.id === assigneeId)

  const create = useMutation({
    mutationFn: (input: CreateIssueInput) => request(CreateIssueMutation, { input }),
    onSuccess: (d) => {
      const created = d.createIssue
      try {
        localStorage.setItem(lastProjectKey, created.project.id)
      } catch {
        // Not remembered; nothing else depends on it.
      }
      toast.show(t('create.created', { identifier: created.identifier }), {
        action: { label: t('common:actions.open'), onClick: () => navigate(`/issue/${created.identifier}`) },
      })
      void qc.invalidateQueries({ queryKey: ['issues'] })
      if (parent) void qc.invalidateQueries({ queryKey: ['children', parent.id] })
      void qc.invalidateQueries({ queryKey: ['issue'] })
      if (createMore) {
        setTitle('')
        setDescription('')
        setErrors({})
        titleRef.current?.focus()
      } else {
        onClose()
      }
    },
    onError: (err) => {
      const field = err instanceof GraphQLRequestError && err.code === 'INVALID_INPUT' && err.field ? err.field : 'form'
      setErrors({ [field === 'title' || field === 'description' || field === 'dueDate' ? field : 'form']: errorMessage(err) })
    },
  })

  const submit = (e?: FormEvent) => {
    e?.preventDefault()
    if (create.isPending) return
    if (!projectId && !parent) {
      setErrors({ form: t('create.chooseProject') })
      return
    }
    const input: CreateIssueInput = { title }
    if (projectId) input.projectId = projectId
    if (parent) input.parentId = parent.id
    if (description.trim()) input.description = description
    if (stateId) input.stateId = stateId
    if (priority) input.priority = priority
    if (assigneeId) input.assigneeId = assigneeId
    if (dueDate) input.dueDate = dueDate
    create.mutate(input)
  }

  const opener = (which: Open) => (e: { currentTarget: HTMLElement }) => {
    setAnchor(e.currentTarget)
    setOpen(open === which ? null : which)
  }
  const close = () => setOpen(null)

  return (
    <form
      className={s.createForm}
      onSubmit={submit}
      onKeyDown={(e) => {
        if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
          e.preventDefault()
          submit()
        }
      }}
    >
      <label className={s.srOnly} htmlFor={ids.title}>
        {t('create.titleLabel')}
      </label>
      <input
        id={ids.title}
        ref={titleRef}
        className={s.createTitle}
        autoFocus
        required
        placeholder={t('create.titlePlaceholder')}
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        aria-invalid={!!errors.title || undefined}
        aria-describedby={errors.title ? ids.titleError : undefined}
      />
      {errors.title && (
        <p id={ids.titleError} role="alert" className={s.fieldError}>
          {errors.title}
        </p>
      )}
      <label className={s.srOnly} htmlFor={ids.desc}>
        {t('create.description')}
      </label>
      <textarea
        id={ids.desc}
        className={s.createDescription}
        rows={5}
        placeholder={t('create.descriptionPlaceholder')}
        value={description}
        onChange={(e) => setDescription(e.target.value)}
      />
      {errors.description && (
        <p role="alert" className={s.fieldError}>
          {errors.description}
        </p>
      )}

      <div className={s.props}>
        {!opts.projectId && !opts.parentId && (
          <Button size="sm" icon={<FolderKanban size={14} aria-hidden />} aria-label={t('create.project')} onClick={opener('project')}>
            {project ? project.key : t('create.chooseProject')}
          </Button>
        )}
        {project && (
          <Button size="sm" aria-label={t('create.state')} icon={state && <StateIcon type={state.type} color={state.color} />} onClick={opener('state')}>
            {state?.name}
          </Button>
        )}
        <Button size="sm" aria-label={t('create.priority')} icon={<PriorityIcon priority={priority} decorative />} onClick={opener('priority')}>
          {t(`common:priority.${priorityKey(priority)}`)}
        </Button>
        <Button size="sm" aria-label={t('create.assignee')} icon={assignee && <Avatar id={assignee.id} name={assignee.name} size={16} decorative />} onClick={opener('assignee')}>
          {assignee ? assignee.name : t('common:pickers.noAssignee')}
        </Button>
        <span className={s.dateField}>
          <CalendarDays size={14} aria-hidden />
          <input type="date" aria-label={t('create.dueDate')} value={dueDate} onChange={(e) => setDueDate(e.target.value)} />
        </span>
        {parent ? (
          <span className={s.chip}>
            <CornerDownRight size={12} aria-hidden />
            <span className={s.chipText}>{t('create.subIssueOf', { identifier: parent.identifier })}</span>
            <button type="button" className={s.chipRemove} aria-label={t('create.removeParent')} onClick={() => setParent(null)}>
              <X size={12} aria-hidden />
            </button>
          </span>
        ) : (
          projectId && (
            <Button size="sm" icon={<CornerDownRight size={14} aria-hidden />} onClick={opener('parent')}>
              {t('create.parent')}
            </Button>
          )
        )}
      </div>
      {errors.dueDate && (
        <p role="alert" className={s.fieldError}>
          {errors.dueDate}
        </p>
      )}

      <Popover anchor={anchor} open={open !== null} onClose={close}>
        {open === 'project' && (
          <ProjectPicker
            value={projectId ?? null}
            onClose={close}
            onSelect={(p) => {
              setProjectId(p.id)
              setStateId(undefined)
              setParent(null)
              close()
            }}
            placeholder={t('create.project')}
          />
        )}
        {open === 'state' && project && (
          <StatePicker
            states={project.states}
            value={state?.id ?? null}
            onClose={close}
            onSelect={(id) => {
              setStateId(id)
              close()
            }}
          />
        )}
        {open === 'priority' && (
          <PriorityPicker
            value={priority}
            onClose={close}
            onSelect={(p) => {
              setPriority(p)
              close()
            }}
          />
        )}
        {open === 'assignee' && viewer && (
          <AssigneePicker
            users={users}
            viewerId={viewer.id}
            value={assigneeId}
            onClose={close}
            onSelect={(id) => {
              setAssigneeId(id)
              close()
            }}
          />
        )}
        {open === 'parent' && projectId && (
          <IssuePicker
            projectId={projectId}
            value={null}
            onClose={close}
            onSelect={(i) => {
              setParent({ id: i.id, identifier: i.identifier })
              close()
            }}
          />
        )}
      </Popover>

      {errors.form && (
        <p id={ids.formError} role="alert" className={s.fieldError}>
          {errors.form}
        </p>
      )}
      <div className={s.createFooter}>
        <label className={s.createMore}>
          <input type="checkbox" checked={createMore} onChange={(e) => setCreateMore(e.target.checked)} />
          {t('create.createMore')}
        </label>
        <Button type="submit" variant="primary" disabled={create.isPending}>
          {t('create.submit')}
        </Button>
      </div>
    </form>
  )
}
