import { GraphQLRequestError, errorMessage } from '../../api/client'
import type { WebhookResource } from '../../gql/graphql'
import { i18n } from '../../i18n'

/** Event types in the order the UI lists them. */
export const resourceOrder: WebhookResource[] = ['ISSUE', 'COMMENT', 'LABEL', 'MILESTONE']

/** Attempts per delivery: the first one plus the retries after 1 minute, 1 hour and 6 hours. */
export const maxAttempts = 4

/** "Issue updated", "Test" and so on; an unknown pair falls back to "resource action". */
export function eventLabel(resource: string, action: string): string {
  const key = `webhooks:event.${resource}.${action}`
  return i18n.exists(key) ? i18n.t(key as 'webhooks:event.issue.create') : `${resource} ${action}`
}

/** Text for an API error on a webhook field; other errors use the shared texts. */
export function webhookErrorMessage(err: unknown): string {
  if (err instanceof GraphQLRequestError && err.code === 'INVALID_INPUT' && err.field && err.reason) {
    const key = `webhooks:errors.${err.field}.${err.reason}`
    if (i18n.exists(key)) return i18n.t(key as 'webhooks:errors.url.too_long', err.params ?? {})
  }
  return errorMessage(err)
}

/** Name of a webhook, or the host of its URL when it has none. */
export function displayName(w: { name: string; url: string }): string {
  if (w.name) return w.name
  try {
    return new URL(w.url).host || w.url
  } catch {
    return w.url
  }
}

const formFields = ['url', 'name', 'projectId', 'resources', 'secret']

/** Form field an API error belongs to, or "form" for errors that name none. */
export function errorField(err: unknown): string {
  const field = err instanceof GraphQLRequestError && err.code === 'INVALID_INPUT' ? err.field : undefined
  return field && formFields.includes(field) ? field : 'form'
}

export type WebhookFormValues = {
  url: string
  name: string
  /** Empty means all projects. */
  projectId: string
  resources: WebhookResource[]
  /** Create only: empty lets the server generate one. */
  secret: string
  enabled: boolean
}

export const emptyWebhookForm: WebhookFormValues = { url: '', name: '', projectId: '', resources: [], secret: '', enabled: true }
