import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate, useNavigate, useParams } from 'react-router'

import { NotFound } from '../app/ProjectRoute'
import { useTitle } from '../app/useTitle'
import { errorMessage, GraphQLRequestError, request } from '../api/client'
import { Activity } from '../features/issue-page/Activity'
import { IssueDescription } from '../features/issue-page/IssueDescription'
import { IssueHeader } from '../features/issue-page/IssueHeader'
import { IssueProperties } from '../features/issue-page/IssueProperties'
import { IssueTitle } from '../features/issue-page/IssueTitle'
import s from '../features/issue-page/issue-page.module.css'
import { MoveIssueDialog } from '../features/issue-page/MoveIssueDialog'
import { IssuePageQuery } from '../features/issue-page/operations'
import { SubIssues } from '../features/issue-page/SubIssues'
import type { PageIssue, PropName } from '../features/issue-page/types'
import { usePageTarget, usePalette } from '../features/command/palette'
import { pushRecent } from '../features/command/recent'
import { useCreateDefaults, useCreateIssue } from '../features/issues/createIssue'
import { DeleteIssueMutation } from '../features/issues/fragments'
import { useEscape, useHotkey } from '../lib/hotkeys'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { useToast } from '../ui/useToast'

export function IssuePage() {
  const { t } = useTranslation()
  const { identifier = '' } = useParams()
  const q = useQuery({ queryKey: ['issue', identifier], queryFn: () => request(IssuePageQuery, { id: identifier }) })
  if (q.error instanceof GraphQLRequestError && q.error.code === 'NOT_FOUND') return <NotFound what="issue" />
  if (q.error) return <p role="alert" className={s.page}>{t('status.loadFailed')}</p>
  if (!q.data) return <p className={s.page}>{t('status.loading')}</p>
  const issue = q.data.issue
  if (issue.identifier !== identifier) return <Navigate replace to={`/issue/${issue.identifier}`} />
  return <IssueView key={issue.id} issue={issue} />
}

/** Back to where the user came from, or to the project when opened directly. */
function useBack(projectKey: string) {
  const navigate = useNavigate()
  return () => {
    const idx = (window.history.state as { idx?: number } | null)?.idx ?? 0
    if (idx > 0) navigate(-1)
    else navigate(`/p/${projectKey}`)
  }
}

function IssueView({ issue }: { issue: PageIssue }) {
  const top = useRef<HTMLDivElement>(null)
  // The shell's scroll area is shared with the list: start the issue at the top.
  useEffect(() => {
    const main = top.current?.closest('main')
    if (main) main.scrollTop = 0
  }, [issue.id])
  const { t } = useTranslation(['issue', 'common'])
  const qc = useQueryClient()
  const toast = useToast()
  const back = useBack(issue.project.key)
  const { openCreateIssue } = useCreateIssue()
  const readOnly = !!issue.project.archivedAt
  const [editing, setEditing] = useState(false)
  const [prop, setProp] = useState<PropName | null>(null)
  const [moving, setMoving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  useTitle(t('docTitle', { identifier: issue.identifier, title: issue.title }))
  useCreateDefaults(readOnly ? null : { projectId: issue.project.id, parentId: issue.id, parentIdentifier: issue.identifier })

  const palette = usePalette()
  const target = { id: issue.id, identifier: issue.identifier, projectId: issue.project.id, projectKey: issue.project.key, title: issue.title }
  usePageTarget(readOnly ? null : [target])
  useEffect(() => pushRecent({ id: issue.id, identifier: issue.identifier, projectId: issue.project.id, projectKey: issue.project.key, title: issue.title }), [issue.id, issue.identifier, issue.project.id, issue.project.key, issue.title])

  const busy = editing || prop !== null || moving || deleting
  useHotkey('e', () => setEditing(true), { enabled: !readOnly && !busy })
  // S/A/P/T/L open the matching palette page for this issue.
  useHotkey('s', () => palette.open('state', [target]), { enabled: !readOnly && !busy })
  useHotkey('a', () => palette.open('assignee', [target]), { enabled: !readOnly && !busy })
  useHotkey('p', () => palette.open('priority', [target]), { enabled: !readOnly && !busy })
  useHotkey('t', () => palette.open('type', [target]), { enabled: !readOnly && !busy })
  useHotkey('l', () => palette.open('labels', [target]), { enabled: !readOnly && !busy })
  useEscape(back, !busy)

  const remove = useMutation({
    mutationFn: () => request(DeleteIssueMutation, { id: issue.id }),
    onSuccess: () => {
      setDeleting(false)
      toast.show(t('delete.done'))
      void Promise.all(['issues', 'children'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
      qc.removeQueries({ queryKey: ['issue', issue.identifier] })
      back()
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })

  return (
    <div ref={top} className={s.page}>
      <IssueHeader
        issue={issue}
        readOnly={readOnly}
        onCreateSubIssue={() => openCreateIssue({ projectId: issue.project.id, parentId: issue.id, parentIdentifier: issue.identifier })}
        onMove={() => setMoving(true)}
        onDelete={() => setDeleting(true)}
      />
      {readOnly && (
        <div className={s.banner} role="status">
          {t('archived')}
        </div>
      )}
      <div className={s.layout}>
        <main className={s.main}>
          <IssueTitle issue={issue} readOnly={readOnly} />
          <IssueDescription issue={issue} readOnly={readOnly} editing={editing} setEditing={setEditing} />
          <SubIssues issue={issue} readOnly={readOnly} />
          <Activity issue={issue} />
        </main>
        <IssueProperties issue={issue} readOnly={readOnly} open={prop} setOpen={setProp} onMove={() => setMoving(true)} />
      </div>
      <MoveIssueDialog issue={issue} open={moving} onClose={() => setMoving(false)} />
      <Dialog open={deleting} onClose={() => setDeleting(false)} title={t('delete.title')}>
        <p>{t('delete.body', { identifier: issue.identifier })}</p>
        <div className={s.dialogActions}>
          <Button onClick={() => setDeleting(false)}>{t('common:actions.cancel')}</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('delete.submit')}
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
