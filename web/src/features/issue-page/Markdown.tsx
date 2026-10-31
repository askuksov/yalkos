import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import s from './issue-page.module.css'

/**
 * GitHub-flavoured Markdown. Raw HTML is not rendered (no rehype-raw) and the
 * default URL transform drops javascript: and other unsafe links; links open
 * in a new tab without an opener.
 */
export function Markdown({ source }: { source: string }) {
  return (
    <div className={s.markdown}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ node: _node, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer" />,
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}
