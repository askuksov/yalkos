import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'

import { errorMessage, request } from '../../api/client'
import { useToast } from '../../ui/useToast'
import { DeleteIssueMutation } from './fragments'

type DeletedListener = (ids: string[]) => void

const listeners = new Set<DeletedListener>()

/** Calls listener with the ids of every issue deleted from now on, until the component unmounts. */
export function useIssuesDeleted(listener: DeletedListener) {
  const current = useRef(listener)
  useEffect(() => {
    current.current = listener
  })
  useEffect(() => {
    const l: DeletedListener = (ids) => current.current(ids)
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  }, [])
}

/**
 * Deletes ids and settles only after every request finished, so a refresh that
 * follows sees the final state even when one request fails early. Returns the
 * ids deleted and the first failure in id order, if any.
 */
async function deleteAll(ids: string[]): Promise<{ deleted: string[]; failure?: unknown }> {
  const results = await Promise.allSettled(ids.map((id) => request(DeleteIssueMutation, { id })))
  const deleted = ids.filter((_, i) => results[i].status === 'fulfilled')
  const failed = results.find((r): r is PromiseRejectedResult => r.status === 'rejected')
  return { deleted, failure: failed?.reason }
}

type Options = {
  /** After every deletion succeeded; count is the number of issues of this run deleted, retries included. */
  onSuccess: (count: number) => void
}

/**
 * Deletes issues for a confirmation (palette, toolbar, issue page):
 * - a retry after a partial failure sends requests only for the issues not
 *   deleted yet;
 * - a second confirmation before the first one settles sends nothing: the
 *   guard is set synchronously, not through the mutation's pending state,
 *   which reaches the component a tick later;
 * - lists drop the deleted issues from their checked set (useIssuesDeleted)
 *   and are refreshed after every run, whatever its outcome.
 */
export function useDeleteIssues({ onSuccess }: Options) {
  const qc = useQueryClient()
  const toast = useToast()
  const deleted = useRef(new Set<string>())
  const running = useRef(false)
  const mutation = useMutation({
    mutationFn: async (ids: string[]) => {
      const { deleted: now, failure } = await deleteAll(ids.filter((id) => !deleted.current.has(id)))
      now.forEach((id) => deleted.current.add(id))
      if (now.length) listeners.forEach((l) => l(now))
      if (failure !== undefined) throw failure
      return ids.filter((id) => deleted.current.has(id)).length
    },
    onSuccess: (count) => onSuccess(count),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => {
      running.current = false
      return Promise.all(['issues', 'children'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
    },
  })
  const run = (ids: string[]) => {
    if (running.current) return
    running.current = true
    mutation.mutate(ids)
  }
  return { run, isPending: mutation.isPending }
}
