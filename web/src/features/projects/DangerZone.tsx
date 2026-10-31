import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import type { CurrentProject } from '../../app/projectContext'
import { errorMessage, request } from '../../api/client'
import { useViewer } from '../../api/hooks'
import { Button } from '../../ui/Button'
import { useToast } from '../../ui/useToast'
import { UnarchiveProjectMutation } from '../issues/fragments'
import { ArchiveProjectMutation, DeleteProjectMutation } from './operations'
import s from './projects.module.css'
import { useInvalidateProjects } from './useInvalidateProjects'

/** Archive / unarchive (admins); delete after typing the key (owner). */
export function DangerZone({ project }: { project: CurrentProject }) {
  const { t } = useTranslation(['projects', 'common'])
  const navigate = useNavigate()
  const toast = useToast()
  const invalidate = useInvalidateProjects()
  const isOwner = useViewer().data?.role === 'OWNER'
  const [confirm, setConfirm] = useState('')
  const onError = (err: unknown) => toast.show(errorMessage(err), { kind: 'error' })
  const archive = useMutation({
    mutationFn: async () => {
      if (project.archivedAt) await request(UnarchiveProjectMutation, { id: project.id })
      else await request(ArchiveProjectMutation, { id: project.id })
    },
    onSuccess: () => invalidate(),
    onError,
  })
  const remove = useMutation({
    mutationFn: () => request(DeleteProjectMutation, { id: project.id, confirmKey: confirm }),
    onSuccess: () => {
      toast.show(t('danger.deleted'))
      void invalidate()
      navigate('/projects', { replace: true })
    },
    onError,
  })
  return (
    <section className={[s.section, s.danger].join(' ')} aria-labelledby="danger-title">
      <h2 id="danger-title">{t('danger.title')}</h2>
      <p className={s.hint}>{t('danger.archiveHint')}</p>
      <div>
        <Button disabled={archive.isPending} onClick={() => archive.mutate()}>
          {project.archivedAt ? t('danger.unarchive') : t('danger.archive')}
        </Button>
      </div>
      {isOwner && (
        <>
          <p className={s.hint}>{t('danger.deleteHint')}</p>
          <div className={s.addRow}>
            <input aria-label={t('danger.confirm', { key: project.key })} placeholder={t('danger.confirm', { key: project.key })} value={confirm} onChange={(e) => setConfirm(e.target.value)} />
            <Button variant="danger" disabled={confirm !== project.key || remove.isPending} onClick={() => remove.mutate()}>
              {t('danger.delete')}
            </Button>
          </div>
        </>
      )}
    </section>
  )
}
