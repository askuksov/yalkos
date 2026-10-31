import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { errorMessage, request } from '../../api/client'
import { Button } from '../../ui/Button'
import { useToast } from '../../ui/useToast'
import { MarkdownEditor } from '../markdown/MarkdownEditor'
import s from './issue-page.module.css'
import { CreateCommentMutation } from './operations'

const draftKey = (issueId: string) => `yalkos.draft.comment.${issueId}`

function readDraft(issueId: string): string {
  try {
    return localStorage.getItem(draftKey(issueId)) ?? ''
  } catch {
    return ''
  }
}

function writeDraft(issueId: string, text: string) {
  try {
    if (text) localStorage.setItem(draftKey(issueId), text)
    else localStorage.removeItem(draftKey(issueId))
  } catch {
    // Storage unavailable: the draft lives only in the field.
  }
}

/** New comment: Markdown with uploads; Cmd/Ctrl+Enter posts; the draft survives a reload. */
export function CommentComposer({ issueId }: { issueId: string }) {
  const { t } = useTranslation('issue')
  const qc = useQueryClient()
  const toast = useToast()
  const [body, setBody] = useState(() => readDraft(issueId))
  const [attachmentIds, setAttachmentIds] = useState<string[]>([])
  const [uploading, setUploading] = useState(false)
  // A fresh editor (and upload list) after each post.
  const [round, setRound] = useState(0)
  const change = (text: string) => {
    setBody(text)
    writeDraft(issueId, text)
  }
  const post = useMutation({
    mutationFn: () => request(CreateCommentMutation, { issueId, body, ...(attachmentIds.length ? { attachmentIds } : {}) }),
    onSuccess: () => {
      change('')
      setAttachmentIds([])
      setRound((r) => r + 1)
      void Promise.all(['history', 'attachments', 'issues', 'children'].map((k) => qc.invalidateQueries({ queryKey: k === 'history' || k === 'attachments' ? [k, issueId] : [k] })))
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })
  const submit = () => {
    if (body.trim() && !post.isPending && !uploading) post.mutate()
  }
  return (
    <div className={s.composer}>
      <MarkdownEditor
        key={round}
        label={t('comments.label')}
        rows={3}
        placeholder={t('comments.placeholder')}
        value={body}
        onChange={change}
        onAttachmentsChange={setAttachmentIds}
        onBusyChange={setUploading}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault()
            submit()
          }
        }}
      />
      <div className={s.editorActions}>
        <Button size="sm" variant="primary" disabled={!body.trim() || post.isPending || uploading} onClick={submit}>
          {t('comments.post')}
        </Button>
      </div>
    </div>
  )
}
