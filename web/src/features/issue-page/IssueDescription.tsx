import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useBlocker } from 'react-router'

import { errorMessage, request } from '../../api/client'
import { UpdateIssueMutation } from '../issues/fragments'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { useToast } from '../../ui/useToast'
import s from './issue-page.module.css'
import { Markdown } from './Markdown'
import type { PageIssue } from './types'

type Props = {
  issue: PageIssue
  readOnly: boolean
  editing: boolean
  setEditing: (v: boolean) => void
}

/**
 * Rendered Markdown; Edit (or E) opens a textarea with Write / Preview.
 * Cmd/Ctrl+Enter saves, Esc cancels and asks first when the text changed;
 * leaving the page with unsaved text asks too.
 */
export function IssueDescription({ issue, readOnly, editing, setEditing }: Props) {
  const { t } = useTranslation('issue')
  const qc = useQueryClient()
  const toast = useToast()
  const [draft, setDraft] = useState(issue.description)
  const [tab, setTab] = useState<'write' | 'preview'>('write')
  const [asking, setAsking] = useState(false)
  const dirty = editing && draft !== issue.description

  const save = useMutation({
    mutationFn: () => request(UpdateIssueMutation, { id: issue.id, input: { description: draft } }),
    onSuccess: () => {
      setEditing(false)
      void qc.invalidateQueries({ queryKey: ['issue'] })
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })

  const blocker = useBlocker(({ currentLocation, nextLocation }) => dirty && currentLocation.pathname !== nextLocation.pathname)

  useEffect(() => {
    if (!dirty) return
    const onUnload = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', onUnload)
    return () => window.removeEventListener('beforeunload', onUnload)
  }, [dirty])

  const start = () => {
    setDraft(issue.description)
    setTab('write')
    setEditing(true)
  }
  const cancel = () => (dirty ? setAsking(true) : setEditing(false))
  const discard = () => {
    setAsking(false)
    setEditing(false)
    if (blocker.state === 'blocked') blocker.proceed()
  }
  const keep = () => {
    setAsking(false)
    if (blocker.state === 'blocked') blocker.reset()
  }

  // E (from the page) opens the editor with the current text.
  const [wasEditing, setWasEditing] = useState(editing)
  if (editing !== wasEditing) {
    setWasEditing(editing)
    if (editing) {
      setDraft(issue.description)
      setTab('write')
    }
  }

  return (
    <section className={s.description} aria-label={t('description.label')}>
      {editing ? (
        <div className={s.editor}>
          <div className={s.tabs} role="tablist">
            {(['write', 'preview'] as const).map((k) => (
              <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}>
                {t(`description.${k}`)}
              </button>
            ))}
          </div>
          {tab === 'write' ? (
            <textarea
              className={s.textarea}
              aria-label={t('description.label')}
              autoFocus
              rows={12}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault()
                  save.mutate()
                } else if (e.key === 'Escape') {
                  e.preventDefault()
                  cancel()
                }
              }}
            />
          ) : draft.trim() ? (
            <Markdown source={draft} />
          ) : (
            <p className={s.muted}>{t('description.nothingToPreview')}</p>
          )}
          <div className={s.editorActions}>
            <Button size="sm" onClick={cancel}>
              {t('description.cancel')}
            </Button>
            <Button size="sm" variant="primary" disabled={save.isPending} onClick={() => save.mutate()}>
              {t('description.save')}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {issue.description.trim() ? <Markdown source={issue.description} /> : <p className={s.muted}>{t('description.empty')}</p>}
          {!readOnly && (
            <Button size="sm" variant="ghost" onClick={start}>
              {t('description.edit')}
            </Button>
          )}
        </>
      )}
      <Dialog open={asking || blocker.state === 'blocked'} onClose={keep} title={t('discard.title')}>
        <p>{t('discard.body')}</p>
        <div className={s.dialogActions}>
          <Button onClick={keep}>{t('discard.keep')}</Button>
          <Button variant="danger" onClick={discard}>
            {t('discard.discard')}
          </Button>
        </div>
      </Dialog>
    </section>
  )
}
