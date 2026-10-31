import { type MdNode, walk } from './mdast'

/** KEY-N as a whole word (project keys: a letter and up to 6 letters/digits). */
const refRe = /(?<![A-Za-z0-9_-])([A-Z][A-Z0-9]{0,6}-[1-9][0-9]*)(?![A-Za-z0-9_])/g

// Text under these nodes is never linked.
const skip = new Set(['code', 'inlineCode', 'link', 'linkReference', 'definition', 'html', 'mention'])

function split(text: string): MdNode[] | null {
  const out: MdNode[] = []
  let last = 0
  for (const m of text.matchAll(refRe)) {
    const at = m.index ?? 0
    if (at > last) out.push({ type: 'text', value: text.slice(last, at) })
    out.push({
      type: 'link',
      url: `/issue/${m[1]}`,
      data: { hProperties: { dataIssueRef: m[1] } },
      children: [{ type: 'text', value: m[1] }],
    })
    last = at + m[0].length
  }
  if (out.length === 0) return null
  if (last < text.length) out.push({ type: 'text', value: text.slice(last) })
  return out
}

/** Remark plugin: issue identifiers in text become links to the issue. */
export function remarkIssueRefs() {
  return (tree: MdNode) => {
    walk(tree, (parent) => {
      if (skip.has(parent.type) || !parent.children) return
      parent.children = parent.children.flatMap((child) => (child.type === 'text' && child.value ? (split(child.value) ?? [child]) : [child]))
    })
  }
}
