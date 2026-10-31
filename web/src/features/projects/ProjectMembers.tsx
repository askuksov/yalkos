import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { CurrentProject } from '../../app/projectContext'
import { errorMessage, request } from '../../api/client'
import { useMembers } from '../../api/hooks'
import { compareNames } from '../../i18n'
import { Avatar } from '../../ui/Avatar'
import { Button } from '../../ui/Button'
import { Popover } from '../../ui/Popover'
import { useToast } from '../../ui/useToast'
import { Picker } from '../../ui/Picker'
import { AddProjectMemberMutation, RemoveProjectMemberMutation } from './operations'
import s from './projects.module.css'
import { useInvalidateProjects } from './useInvalidateProjects'

/** Project members: admins add (member picker) and remove. */
export function ProjectMembers({ project, readOnly }: { project: CurrentProject; readOnly: boolean }) {
  const { t } = useTranslation(['projects', 'common'])
  const toast = useToast()
  const invalidate = useInvalidateProjects()
  const users = useMembers().data ?? []
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const [open, setOpen] = useState(false)
  const change = useMutation({
    mutationFn: async ({ userId, add }: { userId: string; add: boolean }) => {
      if (add) await request(AddProjectMemberMutation, { projectId: project.id, userId })
      else await request(RemoveProjectMemberMutation, { projectId: project.id, userId })
    },
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => invalidate(),
  })
  const memberIds = new Set(project.members.map((m) => m.id))
  return (
    <section className={s.section} aria-labelledby="members-title">
      <h2 id="members-title">{t('members.title')}</h2>
      <ul className={s.list}>
        {project.members.map((m) => (
          <li key={m.id} className={s.item}>
            <Avatar id={m.id} name={m.name} decorative />
            <span className={s.name}>{m.name}</span>
            {!readOnly && (
              <Button size="sm" variant="ghost" aria-label={t('members.removeOf', { name: m.name })} onClick={() => change.mutate({ userId: m.id, add: false })}>
                {t('members.remove')}
              </Button>
            )}
          </li>
        ))}
      </ul>
      {!readOnly && (
        <div>
          <Button ref={setAnchor} size="sm" onClick={() => setOpen(!open)}>
            {t('members.add')}
          </Button>
          <Popover anchor={anchor} open={open} onClose={() => setOpen(false)}>
            <Picker
              items={users.filter((u) => !u.disabled && !memberIds.has(u.id)).sort((a, b) => compareNames(a.name, b.name))}
              getKey={(u) => u.id}
              getLabel={(u) => u.name}
              renderItem={(u) => (
                <>
                  <Avatar id={u.id} name={u.name} size={18} decorative />
                  <span>{u.name}</span>
                </>
              )}
              onClose={() => setOpen(false)}
              onSelect={(u) => {
                setOpen(false)
                change.mutate({ userId: u.id, add: true })
              }}
              placeholder={t('members.add')}
            />
          </Popover>
        </div>
      )}
    </section>
  )
}
