import { request } from '../../api/client'
import { DeleteIssueMutation } from './fragments'

/**
 * Deletes every issue and settles only after all requests finish, so a refresh
 * that follows sees the final state even when one request fails early.
 * Throws the first failure in id order.
 */
export async function deleteIssues(ids: string[]): Promise<void> {
  const results = await Promise.allSettled(ids.map((id) => request(DeleteIssueMutation, { id })))
  const failed = results.find((r): r is PromiseRejectedResult => r.status === 'rejected')
  if (failed) throw failed.reason
}
