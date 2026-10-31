import { type InfiniteData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, File, FileText, Paperclip, Trash2 } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { errorMessage, request } from '../../api/client'
import { useIsAdmin, useViewer } from '../../api/hooks'
import type { IssueTimelineQuery as TimelineData } from '../../gql/graphql'
import { formatNumber } from '../../i18n'
import { formatDateTime } from '../../lib/date'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { useToast } from '../../ui/useToast'
import { useAttach } from './useAttach'
import s from './issue-page.module.css'
import { DeleteAttachmentMutation, IssueAttachmentsQuery } from './operations'

type Att = { id: string; name: string; size: number; contentType: string; url: string; inline: boolean; createdAt: string; uploader: { id: string; name: string } }

/** "2 KB" style size in the UI locale. */
function useSize() {
  const { t } = useTranslation('issue')
  return (n: number) => {
    if (n < 1024) return t('attachments.size.b', { n: formatNumber(n) })
    if (n < 1024 * 1024) return t('attachments.size.kb', { n: formatNumber(Math.round(n / 1024)) })
    return t('attachments.size.mb', { n: formatNumber(Math.round((n / 1024 / 1024) * 10) / 10) })
  }
}

/** The issue's files: preview or icon, name, size, uploader, time; download; delete with a warning when used in text. */
export function Attachments({ issueId, description, readOnly }: { issueId: string; description: string; readOnly: boolean }) {
  const { t } = useTranslation('issue')
  const qc = useQueryClient()
  const toast = useToast()
  const viewer = useViewer().data
  const admin = useIsAdmin()
  const size = useSize()
  const attach = useAttach(issueId)
  const input = useRef<HTMLInputElement>(null)
  const list = useQuery({ queryKey: ['attachments', issueId], queryFn: () => request(IssueAttachmentsQuery, { id: issueId }), select: (d) => d.issue.attachments })
  const [deleting, setDeleting] = useState<Att | null>(null)
  const remove = useMutation({
    mutationFn: (id: string) => request(DeleteAttachmentMutation, { id }),
    onSuccess: () => setDeleting(null),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: ['attachments', issueId] }), qc.invalidateQueries({ queryKey: ['history', issueId] })]),
  })
  // Is the file linked from the description or a loaded comment?
  const used = (a: Att) => {
    const pages = qc.getQueryData<InfiniteData<TimelineData>>(['history', issueId])?.pages ?? []
    const comments = pages.flatMap((p) => p.issue.timeline.nodes).flatMap((n) => (n.__typename === 'Comment' ? [n.body] : []))
    const path = `/api/attachments/${a.id}/`
    return [description, ...comments].some((text) => text.includes(path))
  }
  const files = list.data ?? []
  return (
    <section className={s.attachments} aria-labelledby="attachments-title">
      <div className={s.sectionHead}>
        <h2 id="attachments-title" className={s.sectionTitle}>
          {t('attachments.title')}
        </h2>
        {!readOnly && (
          <>
            <Button size="sm" variant="ghost" icon={<Paperclip size={14} aria-hidden />} onClick={() => input.current?.click()}>
              {t('attachments.attach')}
            </Button>
            <input
              ref={input}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                attach(Array.from(e.target.files ?? []))
                e.target.value = ''
              }}
            />
          </>
        )}
      </div>
      {files.length === 0 ? (
        <p className={s.muted}>{t('attachments.empty')}</p>
      ) : (
        <ul className={s.fileList}>
          {files.map((a) => (
            <li key={a.id} className={s.file}>
              {a.inline && a.contentType.startsWith('image/') ? (
                <img src={a.url} alt="" className={s.thumb} loading="lazy" />
              ) : a.contentType === 'application/pdf' ? (
                <FileText size={20} aria-hidden />
              ) : (
                <File size={20} aria-hidden />
              )}
              <span className={s.fileInfo}>
                <a href={a.url} target={a.inline ? '_blank' : undefined} rel="noopener noreferrer" download={a.inline ? undefined : a.name}>
                  {a.name}
                </a>
                <span className={s.muted}>
                  <span>{size(a.size)}</span> · <span>{t('attachments.by', { name: a.uploader.name, date: formatDateTime(a.createdAt) })}</span>
                </span>
              </span>
              <a className={s.iconButton} href={a.url} download={a.name} aria-label={t('attachments.download', { name: a.name })}>
                <Download size={14} aria-hidden />
              </a>
              {!readOnly && (admin || a.uploader.id === viewer?.id) && (
                <button type="button" className={s.iconButton} aria-label={t('attachments.delete', { name: a.name })} onClick={() => setDeleting(a)}>
                  <Trash2 size={14} aria-hidden />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      <Dialog open={!!deleting} onClose={() => setDeleting(null)} title={t('attachments.deleteTitle', { name: deleting?.name ?? '' })}>
        <p>{t('attachments.deleteBody')}</p>
        {deleting && used(deleting) && <p role="alert">{t('attachments.used')}</p>}
        <div className={s.dialogActions}>
          <Button onClick={() => setDeleting(null)}>{t('comments.cancel')}</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => deleting && remove.mutate(deleting.id)}>
            {t('attachments.deleteSubmit')}
          </Button>
        </div>
      </Dialog>
    </section>
  )
}
