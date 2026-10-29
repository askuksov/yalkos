import type { TypedDocumentNode } from '@graphql-typed-document-node/core'
import { print } from 'graphql'

/** Error returned by the API (errors[0]), with its extensions.code. */
export class GraphQLRequestError extends Error {
  readonly code: string
  readonly field?: string

  constructor(code: string, message: string, field?: string) {
    super(message)
    this.name = 'GraphQLRequestError'
    this.code = code
    this.field = field
  }
}

interface GraphQLResponse<T> {
  data?: T | null
  errors?: { message: string; extensions?: { code?: string; field?: string } }[]
  /** Plain HTTP-level error from the server (e.g. origin check). */
  error?: string
}

/** Sends a typed document to the native API with the session cookie. */
export async function request<TResult, TVars>(
  doc: TypedDocumentNode<TResult, TVars>,
  vars?: TVars,
): Promise<TResult> {
  const res = await fetch('/api/graphql', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query: print(doc), variables: vars ?? {} }),
  })
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
    throw new GraphQLRequestError(err.extensions?.code ?? 'UNKNOWN', err.message, err.extensions?.field)
  }
  if (!body.data) {
    throw new GraphQLRequestError('UNKNOWN', 'Empty response')
  }
  return body.data
}

/** Human message for an error shown in forms. */
export function errorMessage(err: unknown): string {
  if (err instanceof GraphQLRequestError) {
    switch (err.code) {
      case 'AUTHENTICATION_ERROR':
        return 'Invalid email or password'
      case 'RATE_LIMITED':
        return 'Too many attempts. Try again in a few minutes.'
      case 'INVITE_INVALID':
        return 'This invite link is invalid or has expired.'
      default:
        return err.message
    }
  }
  return 'Something went wrong. Try again.'
}
