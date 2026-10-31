import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { IssueRef } from '../markdown/IssueRef'
import { Mention } from '../markdown/Mention'
import { remarkIssueRefs } from '../markdown/remarkIssueRefs'
import { remarkMentions } from '../markdown/remarkMentions'
import { urlTransform } from '../markdown/urlTransform'
import s from './issue-page.module.css'

const plugins = [remarkGfm, remarkMentions, remarkIssueRefs]

const components: Components = {
  a: ({ node: _node, ...props }) => {
    const ref = (props as Record<string, unknown>)['data-issue-ref']
    if (typeof ref === 'string') return <IssueRef identifier={ref} />
    return <a {...props} target="_blank" rel="noopener noreferrer" />
  },
  span: ({ node: _node, ...props }) => {
    const id = (props as Record<string, unknown>)['data-mention']
    if (typeof id === 'string') return <Mention userId={id}>{props.children}</Mention>
    return <span {...props} />
  },
}

/**
 * GitHub-flavoured Markdown with issue links (KEY-N) and mentions. Raw HTML
 * is not rendered (no rehype-raw); only relative, http(s), mailto and mention
 * URLs survive; links open in a new tab without an opener. Rendering issue
 * links and mentions needs the router and query providers.
 */
export function Markdown({ source }: { source: string }) {
  return (
    <div className={s.markdown}>
      <ReactMarkdown remarkPlugins={plugins} skipHtml urlTransform={urlTransform} components={components}>
        {source}
      </ReactMarkdown>
    </div>
  )
}
