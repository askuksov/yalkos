import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'

import { GraphQLRequestError, request } from '../../api/client'
import { StateIcon } from '../../ui/StateIcon'
import s from './markdown.module.css'
import { IssueLinkQuery } from './operations'

/**
 * KEY-N in Markdown: a link to the issue with its state icon and the title on
 * hover; plain text when no such issue exists.
 */
export function IssueRef({ identifier }: { identifier: string }) {
  const q = useQuery({
    queryKey: ['issueRef', identifier],
    queryFn: () => request(IssueLinkQuery, { id: identifier }),
    select: (d) => d.issue,
    staleTime: 60_000,
  })
  if (q.error instanceof GraphQLRequestError && q.error.code === 'NOT_FOUND') return <>{identifier}</>
  const issue = q.data
  return (
    <Link to={`/issue/${identifier}`} className={s.issueRef} title={issue?.title}>
      {issue && <StateIcon type={issue.state.type} color={issue.state.color} size={12} />}
      {identifier}
    </Link>
  )
}
