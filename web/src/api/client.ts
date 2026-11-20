import type { TypedDocumentNode } from '@graphql-typed-document-node/core'
import { print } from 'graphql'

import { i18n } from '../i18n'

type ErrorDetails = { field?: string; reason?: string; params?: Record<string, unknown> }

/** Error returned by the API (errors[0]), with its extensions. */
export class GraphQLRequestError extends Error {
  readonly code: string
  readonly field?: string
  /** INVALID_INPUT: machine-readable reason, e.g. too_long. */
  readonly reason?: string
  /** INVALID_INPUT: values for the reason text, e.g. {max: 255}. */
  readonly params?: Record<string, unknown>

  constructor(code: string, message: string, details: ErrorDetails = {}) {
    super(message)
    this.name = 'GraphQLRequestError'
    this.code = code
    this.field = details.field
    this.reason = details.reason
    this.params = details.params
  }
}

interface GraphQLResponse<T> {
  data?: T | null
  errors?: { message: string; extensions?: { code?: string } & ErrorDetails }[]
  /** Plain HTTP-level error from the server (e.g. origin check). */
  error?: string
}

/**
 * How long a request may take, answer included, before it counts as failed
 * (TIMEOUT). It bounds every API request, so a hung one cannot hold back an
 * error message or a refresh that waits for it; file uploads do not use
 * request and are not bound by it. A write that timed out may still have
 * been applied: callers refresh what they show either way.
 */
export const REQUEST_TIMEOUT_MS = 30_000

/** Sends a typed document to the native API with the session cookie. */
export async function request<TResult, TVars>(
  doc: TypedDocumentNode<TResult, TVars>,
  vars?: TVars,
): Promise<TResult> {
  const timeout = new AbortController()
  const timer = setTimeout(() => timeout.abort(), REQUEST_TIMEOUT_MS)
  const failed = (message: string) =>
    timeout.signal.aborted ? new GraphQLRequestError('TIMEOUT', 'Request timed out') : new GraphQLRequestError('NETWORK_ERROR', message)
  let res: Response
  let body: GraphQLResponse<TResult>
  try {
    try {
      res = await fetch('/api/graphql', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: print(doc), variables: vars ?? {} }),
        signal: timeout.signal,
      })
    } catch {
      throw failed('Network error')
    }
    try {
      body = (await res.json()) as GraphQLResponse<TResult>
    } catch {
      throw failed(`Request failed (${res.status})`)
    }
  } finally {
    clearTimeout(timer)
  }
  if (body.error) {
    throw new GraphQLRequestError(`HTTP_${res.status}`, body.error)
  }
  const err = body.errors?.[0]
  if (err) {
    const { code, field, reason, params } = err.extensions ?? {}
    throw new GraphQLRequestError(code ?? 'UNKNOWN', err.message, { field, reason, params })
  }
  if (!body.data) {
    throw new GraphQLRequestError('UNKNOWN', 'Empty response')
  }
  return body.data
}

/**
 * Text for an error shown to the user: field errors by reason (with params),
 * other API errors by code; the server's English message only when the UI has
 * no text for the code or reason.
 */
export function errorMessage(err: unknown): string {
  if (!(err instanceof GraphQLRequestError)) return i18n.t('errors:generic')
  if (err.code === 'INVALID_INPUT') {
    const key = `errors:reasons.${err.reason ?? ''}`
    return err.reason && i18n.exists(key) ? i18n.t(key as 'errors:reasons.required', err.params ?? {}) : err.message
  }
  const key = `errors:codes.${err.code}`
  return i18n.exists(key) ? i18n.t(key as 'errors:codes.NOT_FOUND') : err.message
}
