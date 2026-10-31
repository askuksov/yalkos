import type { IssueTarget } from './recent'

/** Palette targets from rendered rows/cards (data-row-id, data-identifier, data-project-*). */
export function targetsFrom(rows: HTMLElement[]): IssueTarget[] {
  return rows.flatMap((r) =>
    r.dataset.rowId && r.dataset.identifier && r.dataset.projectId
      ? [{ id: r.dataset.rowId, identifier: r.dataset.identifier, projectId: r.dataset.projectId, projectKey: r.dataset.projectKey }]
      : [],
  )
}
