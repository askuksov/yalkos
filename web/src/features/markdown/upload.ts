import { GraphQLRequestError } from '../../api/client'

/** What POST /api/attachments returns. */
export type UploadResult = {
  id: string
  /** Relative download URL, the form stored in Markdown. */
  url: string
  name: string
  size: number
  contentType: string
  /** Shown in the browser (raster images, PDF) rather than downloaded. */
  inline: boolean
}

type ErrorBody = { error?: string; field?: string; reason?: string; params?: Record<string, unknown> }

const codeByStatus: Record<number, string> = { 401: 'AUTHENTICATION_ERROR', 403: 'FORBIDDEN', 404: 'NOT_FOUND' }

/**
 * Uploads a file over plain HTTP with the session cookie. Without issueId it
 * stays pending until the issue or comment that names it is saved. Errors use
 * the GraphQL error shape so errorMessage() and SessionWatcher understand them.
 */
export async function uploadFile(file: File, issueId?: string): Promise<UploadResult> {
  const form = new FormData()
  // The server streams the body: the issue id must come before the file.
  if (issueId) form.append('issueId', issueId)
  form.append('file', file, file.name)
  let res: Response
  try {
    res = await fetch('/api/attachments', { method: 'POST', credentials: 'same-origin', body: form })
  } catch {
    throw new GraphQLRequestError('NETWORK_ERROR', 'Network error')
  }
  let body: unknown
  try {
    body = await res.json()
  } catch {
    throw new GraphQLRequestError('NETWORK_ERROR', `Upload failed (${res.status})`)
  }
  if (res.ok) return body as UploadResult
  const err = (body ?? {}) as ErrorBody
  const message = err.error ?? `Upload failed (${res.status})`
  if (err.reason) {
    throw new GraphQLRequestError('INVALID_INPUT', message, { field: err.field, reason: err.reason, params: err.params })
  }
  throw new GraphQLRequestError(codeByStatus[res.status] ?? (res.status >= 500 ? 'INTERNAL_ERROR' : `HTTP_${res.status}`), message)
}
