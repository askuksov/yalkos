import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { errorMessage, request } from '../../api/client'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { useToast } from '../../ui/useToast'
import { type PickerProject, ProjectPicker } from '../pickers/ProjectPicker'
import s from './issue-page.module.css'
import { MoveIssueMutation } from './operations'
import type { PageIssue } from './types'

/** Pick the target project, confirm the new identifier, move, open the moved issue. */
export function MoveIssueDialog({ issue, open, onClose }: { issue: PageIssue; open: boolean; onClose: () => void }) {
  const { t } = useTranslation('issue')
  return (
    <Dialog open={open} onClose={onClose} title={t('move.title')}>
      <MoveSteps issue={issue} onClose={onClose} />
    </Dialog>
  )
}

function MoveSteps({ issue, onClose }: { issue: PageIssue; onClose: () => void }) {
  const { t } = useTranslation('issue')
  const qc = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const [target, setTarget] = useState<PickerProject | null>(null)
  const move = useMutation({
    mutationFn: (projectId: string) => request(MoveIssueMutation, { id: issue.id, projectId }),
    onSuccess: (d) => {
      onClose()
      void Promise.all(['issues', 'children', 'issue', 'history'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
      navigate(`/issue/${d.moveIssue.identifier}`, { replace: true })
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
  })
  if (!target) {
    return <ProjectPicker value={null} excludeId={issue.project.id} onSelect={setTarget} onClose={onClose} placeholder={t('move.pick')} />
  }
  const withChildren = issue.children.totalCount > 0
  return (
    <>
      <p>{t(withChildren ? 'move.confirmWithChildren' : 'move.confirm', { identifier: issue.identifier, key: target.key })}</p>
      <div className={s.dialogActions}>
        <Button onClick={() => setTarget(null)}>{t('move.back')}</Button>
        <Button variant="primary" disabled={move.isPending} onClick={() => move.mutate(target.id)}>
          {t('move.submit')}
        </Button>
      </div>
    </>
  )
}
