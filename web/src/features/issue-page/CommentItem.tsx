import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Pencil, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { errorMessage, request } from '../../api/client'
import { useIsAdmin, useViewer } from '../../api/hooks'
import { formatDateTime } from '../../lib/date'
import { Avatar } from '../../ui/Avatar'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { useToast } from '../../ui/useToast'
import { MarkdownEditor } from '../markdown/MarkdownEditor'
import s from './issue-page.module.css'
import { Markdown } from './Markdown'
import { DeleteCommentMutation, UpdateCommentMutation } from './operations'

type CommentNode = { id: string; body: string; createdAt: string; editedAt?: string | null; author: { id: string; name: string } }

/** A comment: author, time, "(edited)", Markdown; the author edits, the author or Owner/Admin delete. */
export function CommentItem({ issueId, comment: c, readOnly }: { issueId: string; comment: CommentNode; readOnly: boolean }) {
  const { t } = useTranslation('issue')
  const qc = useQueryClient()
  const toast = useToast()
  const viewer = useViewer().data
  const admin = useIsAdmin()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(c.body)
  const [deleting, setDeleting] = useState(false)
  const [uploading, setUploading] = useState(false)
  const refresh = () => Promise.all([qc.invalidateQueries({ queryKey: ['history', issueId] }), qc.invalidateQueries({ queryKey: ['attachments', issueId] })])
  const save = useMutation({
    mutationFn: () => request(UpdateCommentMutation, { id: c.id, body: draft }),
    onSuccess: () => setEditing(false),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: () => request(DeleteCommentMutation, { id: c.id }),
    onSuccess: () => {
      setDeleting(false)
      void qc.invalidateQueries({ queryKey: ['issues'] })
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: refresh,
  })
  const mine = viewer?.id === c.author.id
  return (
    <li className={s.comment}>
      <div className={s.commentHead}>
        <Avatar id={c.author.id} name={c.author.name} size={18} decorative />
        <span className={s.commentAuthor}>{c.author.name}</span>
        <time dateTime={c.createdAt} className={s.muted}>
          {formatDateTime(c.createdAt)}
        </time>
        {c.editedAt && <span className={s.muted}>{t('comments.edited')}</span>}
        <span className={s.grow} />
        {!readOnly && mine && !editing && (
          <button
            type="button"
            className={s.iconButton}
            aria-label={t('comments.edit')}
            onClick={() => {
              setDraft(c.body)
              setEditing(true)
            }}
          >
            <Pencil size={12} aria-hidden />
          </button>
        )}
        {!readOnly && (mine || admin) && (
          <button type="button" className={s.iconButton} aria-label={t('comments.delete')} onClick={() => setDeleting(true)}>
            <Trash2 size={12} aria-hidden />
          </button>
        )}
      </div>
      {editing ? (
        <div className={s.editor}>
          <MarkdownEditor
            label={t('comments.edit')}
            autoFocus
            rows={4}
            value={draft}
            onChange={setDraft}
            issueId={issueId}
            onBusyChange={setUploading}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                if (draft.trim() && !uploading) save.mutate()
              } else if (e.key === 'Escape') {
                e.preventDefault()
                setEditing(false)
              }
            }}
          />
          <div className={s.editorActions}>
            <Button size="sm" onClick={() => setEditing(false)}>
              {t('comments.cancel')}
            </Button>
            <Button size="sm" variant="primary" disabled={!draft.trim() || save.isPending || uploading} onClick={() => save.mutate()}>
              {t('comments.save')}
            </Button>
          </div>
        </div>
      ) : (
        <Markdown source={c.body} />
      )}
      <Dialog open={deleting} onClose={() => setDeleting(false)} title={t('comments.deleteTitle')}>
        <p>{t('comments.deleteBody')}</p>
        <div className={s.dialogActions}>
          <Button onClick={() => setDeleting(false)}>{t('comments.cancel')}</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('comments.deleteSubmit')}
          </Button>
        </div>
      </Dialog>
    </li>
  )
}
