import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Command } from 'cmdk'
import { type ReactNode, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { request } from '../../api/client'
import { useProjects } from '../../api/hooks'
import { LogoutMutation } from '../../api/operations'
import { startSession } from '../../auth/session'
import { graphql } from '../../gql'
import { compareNames } from '../../i18n'
import { useHotkey } from '../../lib/hotkeys'
import { setTheme } from '../../theme/theme'
import { Dialog } from '../../ui/Dialog'
import { StateIcon } from '../../ui/StateIcon'
import { useToast } from '../../ui/useToast'
import { useCreateIssue } from '../issues/createIssue'
import s from './command.module.css'
import { type PaletteApi, PaletteContext, type PalettePage } from './palette'
import { AssigneePage, DeletePage, DuePage, LabelsPage, MovePage, ParentPage, PriorityPage, StatePage, TypePage } from './pages'
import { getRecent, type IssueTarget } from './recent'
import { PaletteSearchQuery, useDebounced } from './search'
import { ShortcutsHelp } from './ShortcutsHelp'

const PaletteIssueQuery = graphql(`
  query PaletteIssue($id: String!) {
    issue(id: $id) { id identifier title project { id key } }
  }
`)

const identifierRe = /^[A-Za-z][A-Za-z0-9]{0,6}-[1-9][0-9]{0,9}$/

type Open = { page: PalettePage; target: IssueTarget[] }

/** Command palette (mod+K, / for search) and the shortcuts help (?). */
export function PaletteProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation('command')
  const [state, setState] = useState<Open | null>(null)
  const [help, setHelp] = useState(false)
  const pageTarget = useRef<IssueTarget[] | null>(null)
  const api = useMemo<PaletteApi>(
    () => ({
      open: (page = 'root', target) => setState({ page, target: target ?? pageTarget.current ?? [] }),
      setPageTarget: (target) => {
        pageTarget.current = target
      },
      showHelp: () => setHelp(true),
    }),
    [],
  )
  useHotkey(
    'mod+k',
    () => {
      if (state) return setState(null)
      // Another dialog (create issue, confirm) owns the keyboard.
      if (document.querySelector('dialog[open]')) return false
      api.open('root')
    },
    { allowInInputs: true },
  )
  useHotkey('/', () => api.open('search'), { enabled: !state })
  useHotkey('?', () => setHelp(true), { enabled: !state && !help })
  return (
    <PaletteContext.Provider value={api}>
      {children}
      <Dialog open={!!state} onClose={() => setState(null)} title={t('label')} bare>
        {state && <Palette initial={state} onClose={() => setState(null)} onHelp={() => setHelp(true)} />}
      </Dialog>
      <ShortcutsHelp open={help} onClose={() => setHelp(false)} />
    </PaletteContext.Provider>
  )
}

function Palette({ initial, onClose, onHelp }: { initial: Open; onClose: () => void; onHelp: () => void }) {
  const { t } = useTranslation('command')
  const [stack, setStack] = useState<PalettePage[]>([initial.page])
  const [q, setQ] = useState('')
  const page = stack[stack.length - 1]
  const targets = initial.target
  const push = (p: PalettePage) => {
    setStack([...stack, p])
    setQ('')
  }
  const pageProps = { q, targets, done: onClose }
  const placeholder = page === 'root' ? t('placeholder') : page === 'search' ? t('searchPlaceholder') : t(`pages.${page}`)
  return (
    <Command
      className={s.palette}
      label={t('label')}
      shouldFilter={false}
      loop
      onKeyDown={(e) => {
        if (e.key === 'Backspace' && !q && stack.length > 1) {
          e.preventDefault()
          setStack(stack.slice(0, -1))
        }
      }}
    >
      {stack.length > 1 && <div className={s.crumb}>{t(`pages.${page as Exclude<PalettePage, 'root' | 'search'>}`)}</div>}
      <Command.Input autoFocus value={q} onValueChange={setQ} placeholder={placeholder} />
      <Command.List>
        <Command.Empty>{t('empty')}</Command.Empty>
        {(page === 'root' || page === 'search') && <RootPage q={q} targets={targets} searchOnly={page === 'search'} push={push} close={onClose} onHelp={onHelp} />}
        {page === 'state' && <StatePage {...pageProps} />}
        {page === 'assignee' && <AssigneePage {...pageProps} />}
        {page === 'priority' && <PriorityPage {...pageProps} />}
        {page === 'type' && <TypePage {...pageProps} />}
        {page === 'labels' && <LabelsPage {...pageProps} />}
        {page === 'due' && <DuePage {...pageProps} />}
        {page === 'parent' && <ParentPage {...pageProps} />}
        {page === 'move' && <MovePage {...pageProps} />}
        {page === 'delete' && <DeletePage {...pageProps} />}
      </Command.List>
    </Command>
  )
}

type RootProps = { q: string; targets: IssueTarget[]; searchOnly: boolean; push: (p: PalettePage) => void; close: () => void; onHelp: () => void }

function RootPage({ q, targets, searchOnly, push, close, onHelp }: RootProps) {
  const { t } = useTranslation(['command', 'common'])
  const navigate = useNavigate()
  const qc = useQueryClient()
  const toast = useToast()
  const { openForPage, openCreateIssue } = useCreateIssue()
  const projects = useProjects().data ?? []
  const text = q.trim()
  const search = useDebounced(text, 150)
  const exact = identifierRe.test(text) ? text.toUpperCase() : null
  const exactIssue = useQuery({ queryKey: ['paletteIssue', exact], queryFn: () => request(PaletteIssueQuery, { id: exact ?? '' }), enabled: !!exact, retry: false })
  const found = useQuery({
    queryKey: ['paletteSearch', search],
    queryFn: () => request(PaletteSearchQuery, { filter: { search }, first: 10 }),
    enabled: !!search,
    placeholderData: keepPreviousData,
  })
  const logout = useMutation({
    mutationFn: () => request(LogoutMutation),
    onSuccess: () => {
      startSession()
      qc.clear()
      navigate('/login', { replace: true })
    },
  })
  const go = (to: string) => {
    navigate(to)
    close()
  }
  const has = (label: string) => label.toLowerCase().includes(text.toLowerCase())
  const exactHit = exact && exactIssue.data ? exactIssue.data.issue : null
  const results = text ? (found.data?.issues.nodes ?? []).filter((i) => i.id !== exactHit?.id) : []
  const recent = text ? [] : getRecent()

  type Item = { key: string; label: string; run: () => void }
  const goTo: Item[] = [
    { key: 'my', label: t('goTo.myIssues'), run: () => go('/my-issues') },
    ...projects
      .filter((p) => p.isMember)
      .sort((a, b) => compareNames(a.name, b.name))
      .map((p) => ({ key: `p-${p.id}`, label: t('goTo.project', { name: p.name }), run: () => go(`/p/${p.key}`) })),
    { key: 'browse', label: t('goTo.browseProjects'), run: () => go('/projects') },
    { key: 'members', label: t('goTo.members'), run: () => go('/settings/members') },
    { key: 'keys', label: t('goTo.apiKeys'), run: () => go('/settings/api-keys') },
  ].filter((i) => has(i.label))
  const actions: Item[] = [
    { key: 'create', label: t('actions.createIssue'), run: () => (close(), openForPage()) },
    {
      key: 'theme',
      label: t('actions.toggleTheme'),
      run: () => {
        setTheme(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark')
        close()
      },
    },
    { key: 'help', label: t('actions.shortcuts'), run: () => (close(), onHelp()) },
    { key: 'logout', label: t('actions.logOut'), run: () => logout.mutate() },
  ].filter((i) => has(i.label))
  const single = targets.length === 1 ? targets[0] : null
  const thisIssue: Item[] = targets.length
    ? [
        { key: 'state', label: t('issue.state'), run: () => push('state') },
        { key: 'assignee', label: t('issue.assignee'), run: () => push('assignee') },
        { key: 'priority', label: t('issue.priority'), run: () => push('priority') },
        { key: 'type', label: t('issue.type'), run: () => push('type') },
        { key: 'labels', label: t('issue.labels'), run: () => push('labels') },
        { key: 'due', label: t('issue.due'), run: () => push('due') },
        ...(new Set(targets.map((x) => x.projectId)).size === 1 ? [{ key: 'parent', label: t('issue.parent'), run: () => push('parent') }] : []),
        ...(single
          ? [
              { key: 'sub', label: t('issue.subIssue'), run: () => (close(), openCreateIssue({ projectId: single.projectId, parentId: single.id, parentIdentifier: single.identifier })) },
              { key: 'move', label: t('issue.move'), run: () => push('move') },
              {
                key: 'copy',
                label: t('issue.copy'),
                run: () => {
                  void navigator.clipboard?.writeText(single.identifier)
                  toast.show(t('common:status.copied'))
                  close()
                },
              },
            ]
          : []),
        { key: 'delete', label: t('issue.delete'), run: () => push('delete') },
      ].filter((i) => has(i.label))
    : []
  return (
    <>
      {exactHit && (
        <Command.Group heading={t('groups.issues')}>
          <Command.Item value={`open-${exactHit.id}`} onSelect={() => go(`/issue/${exactHit.identifier}`)}>
            <span className={s.label}>{t('open', { identifier: exactHit.identifier })}</span>
          </Command.Item>
        </Command.Group>
      )}
      {results.length > 0 && (
        <Command.Group heading={exactHit ? undefined : t('groups.issues')}>
          {results.map((i) => (
            <Command.Item key={i.id} value={i.id} onSelect={() => go(`/issue/${i.identifier}`)}>
              <StateIcon type={i.state.type} color={i.state.color} label={i.state.name} />
              <span className={s.identifier}>{i.identifier}</span>
              <span className={s.label}>{i.title}</span>
            </Command.Item>
          ))}
        </Command.Group>
      )}
      {recent.length > 0 && (
        <Command.Group heading={t('groups.recent')}>
          {recent.map((r) => (
            <Command.Item key={r.id} value={`recent-${r.id}`} onSelect={() => go(`/issue/${r.identifier}`)}>
              <span className={s.identifier}>{r.identifier}</span>
              {r.title && <span className={s.label}>{r.title}</span>}
            </Command.Item>
          ))}
        </Command.Group>
      )}
      {!searchOnly && thisIssue.length > 0 && (
        <Command.Group heading={single ? t('groups.thisIssue') : t('groups.theseIssues', { count: targets.length })}>
          {thisIssue.map((i) => (
            <Command.Item key={i.key} value={i.key} onSelect={i.run}>
              <span className={s.label}>{i.label}</span>
            </Command.Item>
          ))}
        </Command.Group>
      )}
      {!searchOnly && goTo.length > 0 && (
        <Command.Group heading={t('groups.goTo')}>
          {goTo.map((i) => (
            <Command.Item key={i.key} value={i.key} onSelect={i.run}>
              <span className={s.label}>{i.label}</span>
            </Command.Item>
          ))}
        </Command.Group>
      )}
      {!searchOnly && actions.length > 0 && (
        <Command.Group heading={t('groups.actions')}>
          {actions.map((i) => (
            <Command.Item key={i.key} value={i.key} onSelect={i.run}>
              <span className={s.label}>{i.label}</span>
            </Command.Item>
          ))}
        </Command.Group>
      )}
    </>
  )
}
