import { type ComponentProps, createContext, useContext } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { IssueRef } from '../markdown/IssueRef'
import { Mention } from '../markdown/Mention'
import { remarkIssueRefs } from '../markdown/remarkIssueRefs'
import { remarkMentions } from '../markdown/remarkMentions'
import { urlTransform } from '../markdown/urlTransform'
import s from './issue-page.module.css'

const plugins = [remarkGfm, remarkMentions, remarkIssueRefs]

// Set inside a link so an image there does not render a nested link.
const InLink = createContext(false)

function sameOrigin(src: string): boolean {
  try {
    return new URL(src, window.location.origin).origin === window.location.origin
  } catch {
    return false
  }
}

function MarkdownImage({ src, alt, ...props }: ComponentProps<'img'>) {
  const inLink = useContext(InLink)
  const url = typeof src === 'string' ? src : ''
  if (url && sameOrigin(url)) return <img src={url} alt={alt} {...props} />
  // Images from other sites are not loaded: the request would tell that site
  // who reads the text and when. An empty url was dropped as unsafe.
  if (!url) return alt ? <>{alt}</> : null
  if (inLink) return <>{alt || url}</>
  return (
    <a href={url} target="_blank" rel="noopener noreferrer">
      {alt || url}
    </a>
  )
}

const components: Components = {
  a: ({ node: _node, ...props }) => {
    const ref = (props as Record<string, unknown>)['data-issue-ref']
    if (typeof ref === 'string') return <IssueRef identifier={ref} />
    return (
      <InLink.Provider value>
        <a {...props} target="_blank" rel="noopener noreferrer" />
      </InLink.Provider>
    )
  },
  img: ({ node: _node, ...props }) => <MarkdownImage {...props} />,
  span: ({ node: _node, ...props }) => {
    const id = (props as Record<string, unknown>)['data-mention']
    if (typeof id === 'string') return <Mention userId={id}>{props.children}</Mention>
    return <span {...props} />
  },
}

/**
 * GitHub-flavoured Markdown with issue links (KEY-N) and mentions. Raw HTML
 * is not rendered (no rehype-raw); only relative, http(s), mailto and mention
 * URLs survive; links open in a new tab without an opener. Only same-origin
 * images load; any other image becomes a link to it. Rendering issue
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
