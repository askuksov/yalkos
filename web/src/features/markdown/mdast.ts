// The few mdast shapes the plugins touch (no dependency on mdast types).

export type MdNode = {
  type: string
  value?: string
  url?: string
  children?: MdNode[]
  data?: { hName?: string; hProperties?: Record<string, unknown> }
}

/** Calls fn for every parent and the index of each child, depth first. */
export function walk(node: MdNode, fn: (parent: MdNode) => void) {
  fn(node)
  for (const child of node.children ?? []) walk(child, fn)
}
