export type IssueTarget = { id: string; identifier: string; projectId: string; projectKey?: string; title?: string }

const key = 'yalkos.recent'
const max = 5

export function getRecent(): IssueTarget[] {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? '[]') as unknown
    return Array.isArray(v) ? v.filter((x): x is IssueTarget => !!x && typeof x.id === 'string' && typeof x.identifier === 'string').slice(0, max) : []
  } catch {
    return []
  }
}

/** Remembers an opened issue (most recent first, at most 5, per browser). */
export function pushRecent(target: IssueTarget): void {
  try {
    const list = [target, ...getRecent().filter((r) => r.id !== target.id)].slice(0, max)
    localStorage.setItem(key, JSON.stringify(list))
  } catch {
    // Storage unavailable: no recent list.
  }
}
