import type { DocumentNode } from 'graphql'
import { vi } from 'vitest'

import { request } from '../api/client'
import { opName } from './render'

// Test files call vi.mock('../api/client', …) so request is a mock here.
export const mocked = vi.mocked(request)

export type Handler = (vars: Record<string, unknown> | undefined) => unknown

/** Routes mocked requests to handlers by operation name. */
export function serve(handlers: Record<string, Handler>) {
  mocked.mockImplementation(async (doc, vars) => {
    const name = opName(doc as unknown as DocumentNode)
    const h = handlers[name]
    if (!h) throw new Error(`unexpected operation ${name}`)
    return h(vars as Record<string, unknown> | undefined) as never
  })
}

/** Variables of every call to one operation. */
export function callsOf(name: string): Record<string, unknown>[] {
  return mocked.mock.calls
    .filter(([doc]) => opName(doc as unknown as DocumentNode) === name)
    .map(([, vars]) => (vars ?? {}) as Record<string, unknown>)
}

/** A promise with its resolve/reject exposed, for controlled response timing. */
export function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}
