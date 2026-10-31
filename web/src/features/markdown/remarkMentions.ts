import { type MdNode, walk } from './mdast'

/**
 * Remark plugin: links to mention:<userId> become mention nodes rendered as a
 * <span data-mention="<userId>"> (the link text is the fallback name).
 */
export function remarkMentions() {
  return (tree: MdNode) => {
    walk(tree, (node) => {
      if (node.type !== 'link' || !node.url?.startsWith('mention:')) return
      const id = node.url.slice('mention:'.length)
      node.type = 'mention'
      node.data = { hName: 'span', hProperties: { dataMention: id } }
      delete node.url
    })
  }
}
