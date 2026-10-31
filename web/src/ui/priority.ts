export const priorityKeys = ['none', 'urgent', 'high', 'medium', 'low'] as const
export type PriorityKey = (typeof priorityKeys)[number]

/** Catalog key for a priority 0–4 (0 none, 1 urgent … 4 low). */
export function priorityKey(p: number): PriorityKey {
  return priorityKeys[p] ?? 'none'
}
