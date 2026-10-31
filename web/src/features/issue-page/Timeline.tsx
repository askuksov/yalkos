import { useInfiniteQuery, useQueries } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { request } from '../../api/client'
import { useMembers, useProjects, useViewer } from '../../api/hooks'
import { ProjectQuery } from '../../api/operations'
import type { IssueTimelineQuery as TimelineData } from '../../gql/graphql'
import { formatDateTime } from '../../lib/date'
import { Button } from '../../ui/Button'
import { useLabels } from '../labels/useLabels'
import { useMilestones } from '../milestones/useMilestones'
import { CommentComposer } from './CommentComposer'
import { CommentItem } from './CommentItem'
import { describe, type HistoryEntry, numbersBefore, referencedIds } from './history'
import s from './issue-page.module.css'
import { IssueRefQuery, IssueTimelineQuery } from './operations'
import type { PageIssue } from './types'

export type TimelineNode = TimelineData['issue']['timeline']['nodes'][number]

/** Query key of an issue's timeline: "history", so every change that refreshes history refreshes it. */
const timelineKey = (issueId: string) => ['history', issueId] as const

/**
 * Comments and changes oldest first; the newest 50 load first and "Show
 * older" pages back. The comment box sits at the end.
 */
export function Timeline({ issue, readOnly }: { issue: PageIssue; readOnly: boolean }) {
  const { t } = useTranslation(['issue', 'common'])
  const viewer = useViewer().data
  const users = useMembers().data
  const projects = useProjects({ includeArchived: true }).data
  const labels = useLabels().data
  const milestones = useMilestones(issue.project.key).data
  const q = useInfiniteQuery({
    queryKey: timelineKey(issue.id),
    queryFn: ({ pageParam }) => request(IssueTimelineQuery, { id: issue.id, before: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => (last.issue.timeline.pageInfo.hasPreviousPage ? last.issue.timeline.pageInfo.startCursor : null),
  })
  const nodes = useMemo(() => [...(q.data?.pages ?? [])].reverse().flatMap((p) => p.issue.timeline.nodes), [q.data])
  const entries = useMemo(() => nodes.filter((n): n is Extract<TimelineNode, { __typename: 'HistoryEntry' }> => n.__typename === 'HistoryEntry'), [nodes])

  // Names for ids in the entries: states of every project the issue was in, parent identifiers.
  const otherProjects = referencedIds(entries, 'projectId').filter((id) => id !== issue.project.id)
  const projectStates = useQueries({
    queries: otherProjects.map((id) => ({ queryKey: ['project', id], queryFn: () => request(ProjectQuery, { id }), retry: false })),
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
    states: new Map([...issue.project.states, ...projectStates.flatMap((x) => x.data?.project.states ?? [])].map((x) => [x.id, x.name])),
    issues: new Map(parents.flatMap((x) => (x.data ? [[x.data.issue.id, x.data.issue.identifier] as const] : []))),
    projects: new Map((projects ?? []).map((p) => [p.id, p.key])),
    numberAt: numbersBefore(entries as HistoryEntry[], issue.number),
    labels: new Map([...(labels?.byId.values() ?? [])].map((l) => [l.id, { name: l.name, type: labels?.isType(l.id) ?? false }])),
    milestones: new Map((milestones ?? []).map((m) => [m.id, m.name])),
  }

  return (
    <section className={s.activity} aria-labelledby="activity-title">
      <h2 id="activity-title" className={s.sectionTitle}>
        {t('activity.title')}
      </h2>
      {q.hasNextPage && (
        <Button size="sm" variant="ghost" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {t('activity.showOlder')}
        </Button>
      )}
      <ol className={s.activityList}>
        {nodes.flatMap((n) =>
          n.__typename === 'Comment'
            ? [<CommentItem key={n.id} issueId={issue.id} comment={n} readOnly={readOnly} />]
            : describe(n as HistoryEntry, ctx).map((line, i) => (
                <li key={`${n.id}-${i}`}>
                  <span>{line}</span>
                  <time dateTime={n.createdAt} className={s.muted}>
                    {formatDateTime(n.createdAt)}
                  </time>
                </li>
              )),
        )}
      </ol>
      {!readOnly && <CommentComposer issueId={issue.id} />}
    </section>
  )
}
