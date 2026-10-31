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

/** Sends a typed document to the native API with the session cookie. */
export async function request<TResult, TVars>(
  doc: TypedDocumentNode<TResult, TVars>,
  vars?: TVars,
): Promise<TResult> {
  let res: Response
  try {
    res = await fetch('/api/graphql', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: print(doc), variables: vars ?? {} }),
    })
  } catch {
    throw new GraphQLRequestError('NETWORK_ERROR', 'Network error')
  }
  let body: GraphQLResponse<TResult>
  try {
    body = (await res.json()) as GraphQLResponse<TResult>
  } catch {
    throw new GraphQLRequestError('NETWORK_ERROR', `Request failed (${res.status})`)
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
