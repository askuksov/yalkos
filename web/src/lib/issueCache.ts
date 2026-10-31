import { type QueryClient, useMutation, useQueryClient } from '@tanstack/react-query'

import { errorMessage, request } from '../api/client'
import { UpdateIssueMutation } from '../features/issues/fragments'
import type { IssueRowFieldsFragment, UpdateIssueInput } from '../gql/graphql'
import { useToast } from '../ui/useToast'

type Node = { id: string }

/** Copies value with every object whose id matches patched; same reference when untouched. */
function patchDeep<T>(value: T, id: string, patch: object): T {
  if (Array.isArray(value)) {
    let changed = false
    const out = value.map((v) => {
      const next = patchDeep(v, id, patch)
      if (next !== v) changed = true
      return next
    })
    return (changed ? out : value) as T
  }
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    let changed = false
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(obj)) {
      const next = patchDeep(v, id, patch)
      if (next !== v) changed = true
      out[k] = next
    }
    if ((obj as Partial<Node>).id === id && 'identifier' in obj) return { ...out, ...patch } as T
    return (changed ? out : value) as T
  }
  return value
}

/**
 * Applies patch to the issue in every list (['issues', …]), sub-issue list
 * (['children', …]) and issue page (['issue', …]) cache. Returns a function
 * that restores the previous data.
 */
export function patchIssue(qc: QueryClient, id: string, patch: Partial<IssueRowFieldsFragment>): () => void {
  const saved: { key: readonly unknown[]; data: unknown }[] = []
  for (const root of ['issues', 'children', 'issue']) {
    for (const q of qc.getQueryCache().findAll({ queryKey: [root] })) {
      const data = q.state.data
      if (data === undefined) continue
      const next = patchDeep(data, id, patch)
      if (next === data) continue
      saved.push({ key: q.queryKey, data })
      qc.setQueryData(q.queryKey, next)
    }
  }
  return () => saved.forEach((s) => qc.setQueryData(s.key, s.data))
}

export type IssueUpdate = {
  id: string
  input: UpdateIssueInput
  /** Shown at once in every cache; rolled back if the server says no. */
  optimistic?: Partial<IssueRowFieldsFragment>
}

const updateKey = ['updateIssue']

/** updateIssue with an optimistic cache patch, rollback + error toast, refetch on settle. */
export function useIssueMutation() {
  const qc = useQueryClient()
  const toast = useToast()
  return useMutation({
    mutationKey: updateKey,
    mutationFn: ({ id, input }: IssueUpdate) => request(UpdateIssueMutation, { id, input }),
    onMutate: async ({ id, optimistic }) => {
      // A refetch already in flight would land with the old value and undo the patch.
      await Promise.all(['issues', 'children', 'issue'].map((k) => qc.cancelQueries({ queryKey: [k] })))
      return { rollback: optimistic ? patchIssue(qc, id, optimistic) : () => {} }
    },
    onError: (err, _v, ctx) => {
      ctx?.rollback()
      toast.show(errorMessage(err), { kind: 'error' })
    },
    // Bulk changes run one mutation per issue: refetch once, after the last one.
    onSettled: () =>
      qc.isMutating({ mutationKey: updateKey }) > 1 ? undefined : Promise.all([
        qc.invalidateQueries({ queryKey: ['issues'] }),
        qc.invalidateQueries({ queryKey: ['children'] }),
        qc.invalidateQueries({ queryKey: ['issue'] }),
        qc.invalidateQueries({ queryKey: ['history'] }),
      ]),
  })
}
