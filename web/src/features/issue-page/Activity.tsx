import { useInfiniteQuery, useQueries } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { request } from '../../api/client'
import { useMembers, useProjects, useViewer } from '../../api/hooks'
import { ProjectQuery } from '../../api/operations'
import { formatDateTime } from '../../lib/date'
import { Button } from '../../ui/Button'
import { describe, numbersBefore, referencedIds } from './history'
import s from './issue-page.module.css'
import { IssueHistoryQuery, IssueRefQuery } from './operations'
import type { PageIssue } from './types'

/** History oldest first; the newest 50 load first, "Show older" pages back. */
export function Activity({ issue }: { issue: PageIssue }) {
  const { t } = useTranslation(['issue', 'common'])
  const viewer = useViewer().data
  const users = useMembers().data
  const projects = useProjects({ includeArchived: true }).data
  const history = useInfiniteQuery({
    queryKey: ['history', issue.id],
    queryFn: ({ pageParam }) => request(IssueHistoryQuery, { id: issue.id, before: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => (last.issue.history.pageInfo.hasPreviousPage ? last.issue.history.pageInfo.startCursor : null),
  })
  const entries = useMemo(() => [...(history.data?.pages ?? [])].reverse().flatMap((p) => p.issue.history.nodes), [history.data])

  // Names for ids in the entries: states of every project the issue was in, parent identifiers.
  const otherProjects = referencedIds(entries, 'projectId').filter((id) => id !== issue.project.id)
  const projectStates = useQueries({
    queries: otherProjects.map((id) => ({
      queryKey: ['project', id],
      queryFn: () => request(ProjectQuery, { id }),
      retry: false,
    })),
  })
  const parents = useQueries({
    queries: referencedIds(entries, 'parentId').map((id) => ({
      queryKey: ['issueRef', id],
      queryFn: () => request(IssueRefQuery, { id }),
      retry: false,
      staleTime: Infinity,
    })),
  })

  const ctx = {
    t,
    viewerId: viewer?.id,
    users: new Map((users ?? []).map((u) => [u.id, u.name])),
    states: new Map([...issue.project.states, ...projectStates.flatMap((q) => q.data?.project.states ?? [])].map((x) => [x.id, x.name])),
    issues: new Map(parents.flatMap((q) => (q.data ? [[q.data.issue.id, q.data.issue.identifier] as const] : []))),
    projects: new Map((projects ?? []).map((p) => [p.id, p.key])),
    numberAt: numbersBefore(entries, issue.number),
  }

  return (
    <section className={s.activity} aria-labelledby="activity-title">
      <h2 id="activity-title" className={s.sectionTitle}>
        {t('activity.title')}
      </h2>
      {history.hasNextPage && (
        <Button size="sm" variant="ghost" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>
          {t('activity.showOlder')}
        </Button>
      )}
      <ol className={s.activityList}>
        {entries.flatMap((e) =>
          describe(e, ctx).map((line, i) => (
            <li key={`${e.id}-${i}`}>
              <span>{line}</span>
              <time dateTime={e.createdAt} className={s.muted}>
                {formatDateTime(e.createdAt)}
              </time>
            </li>
          )),
        )}
      </ol>
    </section>
  )
}
