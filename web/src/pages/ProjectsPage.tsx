import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { PageHeader } from '../app/PageHeader'
import { errorMessage, request } from '../api/client'
import { useIsAdmin, useViewer } from '../api/hooks'
import { UnarchiveProjectMutation } from '../features/issues/fragments'
import { CreateProjectDialog } from '../features/projects/CreateProjectDialog'
import { AddProjectMemberMutation, BrowseProjectsQuery, RemoveProjectMemberMutation } from '../features/projects/operations'
import s from '../features/projects/projects.module.css'
import { useInvalidateProjects } from '../features/projects/useInvalidateProjects'
import { compareNames } from '../i18n'
import { Button } from '../ui/Button'
import { useToast } from '../ui/useToast'

/** All projects: join/leave; admins create projects and see the archived ones. */
export function ProjectsPage() {
  const { t } = useTranslation(['projects', 'common'])
  const isAdmin = useIsAdmin()
  const viewer = useViewer().data
  const toast = useToast()
  const invalidate = useInvalidateProjects()
  const [creating, setCreating] = useState(false)
  const projects = useQuery({
    queryKey: ['browseProjects'],
    queryFn: () => request(BrowseProjectsQuery, { includeArchived: true }),
    select: (d) => [...d.projects.nodes].sort((a, b) => compareNames(a.name, b.name)),
  })
  const onError = (err: unknown) => toast.show(errorMessage(err), { kind: 'error' })
  const membership = useMutation({
    mutationFn: async ({ projectId, join }: { projectId: string; join: boolean }) => {
      const vars = { projectId, userId: viewer?.id ?? '' }
      if (join) await request(AddProjectMemberMutation, vars)
      else await request(RemoveProjectMemberMutation, vars)
    },
    onSuccess: () => invalidate(),
    onError,
  })
  const unarchive = useMutation({ mutationFn: (id: string) => request(UnarchiveProjectMutation, { id }), onSuccess: () => invalidate(), onError })
  const all = projects.data ?? []
  const active = all.filter((p) => !p.archivedAt)
  const archived = all.filter((p) => p.archivedAt)

  return (
    <>
      <PageHeader title={t('browse.title')}>
        {isAdmin && (
          <Button size="sm" variant="primary" onClick={() => setCreating(true)}>
            {t('browse.create')}
          </Button>
        )}
      </PageHeader>
      <div className={s.content}>
        {projects.data && !active.length && <p className={s.muted}>{t('browse.none')}</p>}
        {active.length > 0 && (
          <ul className={s.list}>
            {active.map((p) => (
              <li key={p.id} className={s.item}>
                <span className={s.key}>{p.key}</span>
                <span className={s.name}>
                  <Link to={`/p/${p.key}`}>{p.name}</Link>
                </span>
                <span className={s.muted}>{t('browse.members', { count: p.members.length })}</span>
                <Button size="sm" variant={p.isMember ? 'secondary' : 'primary'} disabled={membership.isPending} onClick={() => membership.mutate({ projectId: p.id, join: !p.isMember })}>
                  {p.isMember ? t('browse.leave') : t('browse.join')}
                </Button>
              </li>
            ))}
          </ul>
        )}
        {isAdmin && archived.length > 0 && (
          <section className={s.section}>
            <h2>{t('browse.archived')}</h2>
            <ul className={s.list}>
              {archived.map((p) => (
                <li key={p.id} className={s.item}>
                  <span className={s.key}>{p.key}</span>
                  <span className={s.name}>
                    <Link to={`/p/${p.key}`}>{p.name}</Link>
                  </span>
                  <Button size="sm" disabled={unarchive.isPending} onClick={() => unarchive.mutate(p.id)}>
                    {t('browse.unarchive')}
                  </Button>
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
      <CreateProjectDialog open={creating} onClose={() => setCreating(false)} takenKeys={new Set(all.map((p) => p.key))} />
    </>
  )
}
