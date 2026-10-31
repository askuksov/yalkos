import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { errorMessage, request } from '../../api/client'
import { useIssueMutation } from '../../lib/issueCache'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { Popover } from '../../ui/Popover'
import { useToast } from '../../ui/useToast'
import { AssigneePicker, type PickerUser } from '../pickers/AssigneePicker'
import { PriorityPicker } from '../pickers/PriorityPicker'
import { type PickerState, StatePicker } from '../pickers/StatePicker'
import { DeleteIssueMutation } from './fragments'
import s from './issues.module.css'

type Props = {
  ids: string[]
  /** Project scope only: states differ between projects. */
  states?: PickerState[]
  users: PickerUser[]
  viewerId: string
  onDone: () => void
}

type Open = 'state' | 'assignee' | 'priority' | null

/** Actions for the checked issues: state, assignee, priority, delete. */
export function BulkBar({ ids, states, users, viewerId, onDone }: Props) {
  const { t } = useTranslation(['issues', 'common'])
  const qc = useQueryClient()
  const toast = useToast()
  const update = useIssueMutation()
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const [open, setOpen] = useState<Open>(null)
  const [confirm, setConfirm] = useState(false)
  const close = () => setOpen(null)
  const finish = () => {
    close()
    onDone()
  }
  const remove = useMutation({
    mutationFn: () => Promise.all(ids.map((id) => request(DeleteIssueMutation, { id }))),
    onSuccess: () => {
      toast.show(t('bulk.deleted', { count: ids.length }))
      setConfirm(false)
      onDone()
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: ['issues'] }), qc.invalidateQueries({ queryKey: ['children'] })]),
  })

  const opener = (which: Open) => (e: { currentTarget: HTMLElement }) => {
    setAnchor(e.currentTarget)
    setOpen(open === which ? null : which)
  }

  return (
    <div className={s.bulk} role="toolbar" aria-label={t('list.selected', { count: ids.length })}>
      <span>{t('list.selected', { count: ids.length })}</span>
      {states && (
        <Button size="sm" onClick={opener('state')}>
          {t('bulk.state')}
        </Button>
      )}
      <Button size="sm" onClick={opener('assignee')}>
        {t('bulk.assignee')}
      </Button>
      <Button size="sm" onClick={opener('priority')}>
        {t('bulk.priority')}
      </Button>
      <Button size="sm" variant="danger" onClick={() => setConfirm(true)}>
        {t('bulk.delete')}
      </Button>
      <Button size="sm" variant="ghost" onClick={onDone}>
        {t('list.clearSelection')}
      </Button>
      <Popover anchor={anchor} open={open !== null} onClose={close} placement="top-start">
        {open === 'state' && states && (
          <StatePicker
            states={states}
            value={null}
            onClose={close}
            onSelect={(stateId) => {
              const st = states.find((x) => x.id === stateId)
              ids.forEach((id) => update.mutate({ id, input: { stateId }, optimistic: st && { state: { id: st.id, name: st.name, type: st.type, color: st.color } } }))
              finish()
            }}
          />
        )}
        {open === 'assignee' && (
          <AssigneePicker
            users={users}
            viewerId={viewerId}
            value={null}
            onClose={close}
            onSelect={(userId) => {
              const u = users.find((x) => x.id === userId)
              ids.forEach((id) => update.mutate({ id, input: { assigneeId: userId }, optimistic: { assignee: u ? { id: u.id, name: u.name } : null } }))
              finish()
            }}
          />
        )}
        {open === 'priority' && (
          <PriorityPicker
            value={-1}
            onClose={close}
            onSelect={(priority) => {
              ids.forEach((id) => update.mutate({ id, input: { priority }, optimistic: { priority } }))
              finish()
            }}
          />
        )}
      </Popover>
      <Dialog open={confirm} onClose={() => setConfirm(false)} title={t('bulk.deleteTitle')}>
        <p>{t('bulk.deleteConfirm', { count: ids.length })}</p>
        <div className={s.dialogActions}>
          <Button onClick={() => setConfirm(false)}>{t('common:actions.cancel')}</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('bulk.delete')}
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
